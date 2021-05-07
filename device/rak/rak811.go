package rak

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"regexp"
	"time"

	"github.com/ipsusila/lorago/atcmd"
	"github.com/ipsusila/lorago/device/serial"
	"github.com/ipsusila/opt"
)

const (
	SectionDevice = "dev"
	SectionLoRa   = "lora"
)

// wake up constant
var WakeUp = []byte("Wake up.\r\n")

type Rak811 struct {
	dev       serial.Device
	at        *atcmd.Handler
	lastError error
	conf      Config
}

// NewRak811 create RAK-811 LoRa device connection
func NewRak811(confFile string) (*Rak811, error) {
	// configuration
	op, err := opt.FromFile(confFile, opt.FormatAuto)
	if err != nil {
		return nil, err
	}

	// serial command
	dev, err := serial.NewConfig(confFile, SectionDevice)
	if err != nil {
		return nil, err
	}

	// create command handler
	at := atcmd.NewDefaultHandler(dev)

	// create new Rak811 device
	ra := Rak811{
		dev: dev,
		at:  at,
	}

	// setup handler
	at.OnError(ra.onError)
	at.OnResponse(ra.onResponse)

	// Default comm timeout
	ra.conf.CommTimeout.Duration = 10 * time.Second
	if err := op.AsStruct(&ra.conf); err != nil {
		return nil, err
	}

	// run loop
	at.Run()

	return &ra, nil
}

// Close connection to device
func (r *Rak811) Close() error {
	if r.at != nil {
		r.at.Close()
		r.at = nil
	}
	if dev := r.dev; dev != nil {
		r.dev = nil
		return dev.Close()
	}
	return nil
}

// OnError handler
func (r *Rak811) onError(err error, done chan<- bool) {
	log.Printf("Device error: %v\n", err)
	r.Close()
	done <- true
}

//OnResponse function
func (r *Rak811) onResponse(data *atcmd.SerialData, err error) {
	log.Printf("@%s CMD: %q, DATA: %q", data.At.Format(time.RFC3339), string(data.Cmd), string(data.Response))
}

// LastError return latest error
func (r *Rak811) LastError() error {
	return r.lastError
}

// IsValid return true if device connection is not closed.
func (r *Rak811) IsValid() bool {
	return r.dev != nil && r.at != nil
}

func (r *Rak811) SendCommandContext(ctx context.Context, cmd string, okPattern, errPattern string, maxWait time.Duration) (string, error) {
	if okPattern == "" {
		okPattern = atcmd.OkPattern
	}
	reOk, err := regexp.Compile(okPattern)
	if err != nil {
		return "", err
	}

	if errPattern == "" {
		errPattern = atcmd.ErrPattern
	}
	reErr, err := regexp.Compile(errPattern)
	if err != nil {
		return "", err
	}
	return r.sendCommandContext(ctx, cmd, reOk, reErr)
}

func (r *Rak811) sendCommandContext(ctx context.Context, cmd string, reOk, reErr *regexp.Regexp) (string, error) {
	done := make(chan bool, 1)
	resend := make(chan bool, 1)
	quit := make(chan bool)
	fnResp := func(data *atcmd.SerialData, err error) {
		fmt.Printf("Incoming data: %q\n", string(data.Response))
		if err != nil {
			close(quit)
		} else if bytes.Compare(data.Response, WakeUp) == 0 {
			resend <- true
		} else if reOk.Match(data.Response) || reErr.Match(data.Response) {
			close(done)
		}

	}
	// setup context for AT+COMMAND
	ctxAt, cancel := context.WithTimeout(ctx, r.conf.CommTimeout.Duration)
	defer cancel()

	// setup command
	var err error
	var response string
	r.at.OnResponse(fnResp)

	id, err := r.at.WriteStringContext(ctxAt, cmd, fnResp)
	if err != nil {
		return "", err
	}
	for {
		select {
		case <-ctx.Done():
			return response, ctx.Err()
		case <-done:
			data := r.at.TakeResponse(id)
			return string(data.Response), nil
		case <-resend:
			id, err = r.at.WriteStringContext(ctxAt, cmd, fnResp)
			if err != nil {
				return "", err
			}
		}
	}
}

// FirmwareVersion return firmware version
func (r *Rak811) FirmwareVersion() (string, error) {
	return r.FirmwareVersionContext(context.TODO())
}

// FirmwareVersionContext check version
func (r *Rak811) FirmwareVersionContext(ctx context.Context) (string, error) {
	return r.sendCommandContext(ctx, atVersion, atcmd.RegexpOk, atcmd.RegexpError)
}

// SendDataContext sends data through LoRa
func (r *Rak811) SendDataContext(ctx context.Context, channel int, data []byte) ([]byte, error) {
	// TODO, OK: pattern, ERROR: Error pattern
	cmd := fmt.Sprintf(atLoraSend, channel, data)
	res, err := r.sendCommandContext(ctx, cmd, atcmd.RegexpOk, atcmd.RegexpError)
	if err != nil {
		return nil, err
	}
	return []byte(res), nil
}

// SendData sends data through LoRa
func (r *Rak811) SendData(ctx context.Context, channel int, data []byte) ([]byte, error) {
	return r.SendDataContext(context.TODO(), channel, data)
}
