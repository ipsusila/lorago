package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/ipsusila/lorago/device/rak"
	gpsd "github.com/stratoberry/go-gpsd"
)

type FnOnData func(td *TrackingData)

// list of gpsmodes
var gpsModes = map[gpsd.Mode]string{
	gpsd.NoValueSeen: "NV",
	gpsd.NoFix:       "NF",
	gpsd.Mode2D:      "2D",
	gpsd.Mode3D:      "3D",
}

type TrackingData struct {
	Mode string    `json:"mode"`
	At   time.Time `json:"at"`
	Lon  float32   `json:"lon"`
	Lat  float32   `json:"lat"`
	Alt  float32   `json:"alt"`
	Spd  float32   `json:"spd"`
}

type Tracker struct {
	sync.Mutex
	ready bool

	quit       chan bool
	done       chan bool
	data       chan *TrackingData
	Timeout    time.Duration
	SentWriter io.Writer
}

func (td *TrackingData) Encode() []byte {
	fmt.Printf("Sending lon=%.5f, lat=%.5f, alt=%f, spd=%f\n",
		td.Lon, td.Lat, td.Alt, td.Spd)
	buf := bytes.Buffer{}
	buf.Write([]byte("@S"))

	// Order of the package (34 bytes)
	bo := binary.BigEndian
	binary.Write(&buf, bo, uint32(td.At.Local().Unix()))
	binary.Write(&buf, bo, td.Lon)
	binary.Write(&buf, bo, td.Lat)
	binary.Write(&buf, bo, uint8(td.Alt))
	binary.Write(&buf, bo, uint8(td.Spd))

	return buf.Bytes()
}

func (td *TrackingData) Heartbeat() []byte {
	return []byte("@OK")
}

func NewTracker() *Tracker {
	tr := Tracker{
		ready:   false,
		quit:    make(chan bool, 1),
		done:    make(chan bool, 1),
		data:    make(chan *TrackingData, 1),
		Timeout: 10 * time.Second,
	}
	return &tr
}

func (t *Tracker) Quit() {
	go close(t.quit)
}

func (t *Tracker) IsReady() bool {
	t.Lock()
	defer t.Unlock()
	return t.ready
}

func (t *Tracker) SetReady(v bool) bool {
	t.Lock()
	defer t.Unlock()
	oldV := t.ready
	t.ready = v

	return oldV
}

func (t *Tracker) Run(confFile string) {
	defer close(t.done)

	r811, err := rak.NewRak811(confFile)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer r811.Close()

	// 1. do initialization
	if status, err := r811.InitializeContext(context.Background()); err != nil {
		fmt.Println("Initialization error:", err)
		return
	} else {
		fmt.Println("Initialization done:", status)
		js, _ := json.MarshalIndent(r811.LastLoraStatus(), "", "  ")
		fmt.Println("=== LoRa STATUS ===")
		fmt.Println(string(js))
	}

	// Join network
	fnJoin := func() bool {
		// 2. join?
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		ls := r811.LastLoraStatus()
		if !ls.JoinedNetwork {
			fmt.Println(" Joining network ....")
			resp, status, err := r811.LoraJoinContext(ctx)
			fmt.Println("=== JOIN ===, status:", status, ", error:", err)
			fmt.Println(resp)
		}
		return ls.JoinedNetwork
	}

	// wait until join success
	joined := r811.LastLoraStatus().JoinedNetwork
	for !joined {
		select {
		case <-t.quit:
			return
		default:
			joined = fnJoin()
			// sleep a moment
			time.Sleep(5 * time.Second)
		}
	}

	// start watching GPS
	go t.watchGpsd()

	fnSendHb := func(timeout time.Duration) {
		hb := TrackingData{}
		t.SetReady(false)
		t.sendData(r811, &hb, timeout)
		t.SetReady(true)
	}
	// send heartbeat at firsttime
	fnSendHb(t.Timeout)

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-t.quit:
			return
		case <-ticker.C:
			fnSendHb(t.Timeout)
		case d := <-t.data:
			t.SetReady(false)
			t.sendData(r811, d, t.Timeout)
			t.SetReady(true)
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func (t *Tracker) sendData(r811 *rak.Rak811, d *TrackingData, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.TODO(), timeout)
	defer cancel()

	encoded := d.Heartbeat()
	if !d.At.IsZero() {
		encoded = d.Encode()
	}
	resp, status, err := r811.SendDataContext(ctx, -1, encoded)
	fmt.Printf("RESP: %s, STATUS: %s, Err: %v\n", resp, status, err)
	if status == rak.StatusOK && !d.At.IsZero() && t.SentWriter != nil {
		//Save
		fmt.Fprintf(t.SentWriter, "%s;%s;%.5f;%.5f;%.1f;%.1f\n",
			d.At.Format(time.RFC3339), d.Mode, d.Lon, d.Lat, d.Alt, d.Spd)
	}
}

func (t *Tracker) watchGpsd() error {
	fmt.Println("Watching GPSD")
	var gps *gpsd.Session
	var err error

	if gps, err = gpsd.Dial(gpsd.DefaultAddress); err != nil {
		return fmt.Errorf("failed to connect to GPSD: %s", err)
	}

	// POSITION Filter
	gps.AddFilter("TPV", func(r interface{}) {
		tpv := r.(*gpsd.TPVReport)
		td := TrackingData{
			At:   tpv.Time,
			Lon:  float32(tpv.Lon),
			Lat:  float32(tpv.Lat),
			Alt:  float32(tpv.Alt),
			Spd:  float32(tpv.Speed),
			Mode: gpsModes[tpv.Mode],
		}
		// send data
		go func() {
			if t.IsReady() {
				t.data <- &td
			}
		}()
	})

	done := gps.Watch()
	select {
	case <-done:
		return nil
	case <-t.quit:
		// Quit request, should stop the socket
		return nil
	}
}

func main() {
	sConf := flag.String("conf", "config.hjson", "Configuratio file")
	vTimeout := flag.Int("timeout", 5, "data send timeout in second")
	sOut := flag.String("out", "out.csv", "output file")
	flag.Parse()

	// open file
	fd, err := os.OpenFile(*sOut, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("Open file error: ", err)
		return
	}
	defer fd.Close()

	tr := NewTracker()
	if *vTimeout > 0 {
		tr.Timeout = time.Duration(*vTimeout) * time.Second
	}
	tr.SentWriter = fd

	// start watching
	go tr.Run(*sConf)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	close(tr.quit)
	fmt.Println("CTRL+C catched!")
	<-tr.done
	fmt.Println("Done application!")
}
