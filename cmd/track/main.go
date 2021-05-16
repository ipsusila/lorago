package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/ipsusila/lorago/device/rak"
	gpsd "github.com/stratoberry/go-gpsd"
)

type FnOnData func(td *TrackingData)

type TrackingData struct {
	At  time.Time `json:"at"`
	Lon float64   `json:"lon"`
	Lat float64   `json:"lat"`
	Alt float32   `json:"alt"`
	Spd float32   `json:"spd"`
}

type Tracker struct {
	sync.Mutex
	ready bool

	quit chan bool
	done chan bool
	data chan *TrackingData
}

func (td *TrackingData) Encode() []byte {
	buf := bytes.Buffer{}
	buf.Write([]byte("@S"))

	// Order of the package (34 bytes)
	bo := binary.BigEndian
	binary.Write(&buf, bo, td.At.Local().UnixNano())
	binary.Write(&buf, bo, td.Lon)
	binary.Write(&buf, bo, td.Lat)
	binary.Write(&buf, bo, td.Alt)
	binary.Write(&buf, bo, td.Spd)

	return buf.Bytes()
}

func (td *TrackingData) Heartbeat() []byte {
	return []byte("@OK")
}

func NewTracker() *Tracker {
	tr := Tracker{
		ready: false,
		quit:  make(chan bool, 1),
		done:  make(chan bool, 1),
		data:  make(chan *TrackingData, 1),
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

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-t.quit:
			return
		case <-ticker.C:
			hb := TrackingData{}
			t.SetReady(false)
			t.sendData(r811, &hb, 15*time.Second)
			t.SetReady(true)
		case d := <-t.data:
			t.SetReady(false)
			t.sendData(r811, d, 15*time.Second)
			t.SetReady(true)
			time.Sleep(1 * time.Second)
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
			At:  tpv.Time,
			Lon: tpv.Lon,
			Lat: tpv.Lat,
			Alt: float32(tpv.Alt),
			Spd: float32(tpv.Speed),
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
	flag.Parse()

	tr := NewTracker()
	go tr.Run(*sConf)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	close(tr.quit)
	fmt.Println("CTRL+C catched!")
	<-tr.done
	fmt.Println("Done application!")
}
