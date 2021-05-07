package rak

import (
	"bytes"
	"context"
	"fmt"
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
var WakeUp = []byte("Wake Up.\r\n")

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
	at, err := atcmd.NewDefaultHandler(dev)
	if err != nil {
		return nil, err
	}

	// create new Rak811 device
	ra := Rak811{
		dev: dev,
		at:  at,
	}
	if err := op.AsStruct(&ra.conf); err != nil {
		return nil, err
	}

	return &ra, nil
}

// Close connection to device
func (r *Rak811) Close() error {
	if dev := r.dev; dev != nil {
		r.dev = nil
		return dev.Close()
	}
	if r.at != nil {
		r.at = nil
	}
	return nil
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
	done := false
	fnResp := func(data []byte) (bool, error) {
		if len(data) > 0 {
			fmt.Printf("Incoming data: %q\n", string(data))
			if bytes.Compare(data, WakeUp) == 0 {
				return true, nil
			} else if reOk.Match(data) || reErr.Match(data) {
				done = true
				return true, nil
			}
		}
		return false, nil
	}
	// setup context for AT+COMMAND
	ctxAt, cancel := context.WithTimeout(ctx, r.conf.CommTimeout.Duration)
	defer cancel()

	// setup command
	var err error
	var response string
	r.at.OnResponse(fnResp)
	for !done {
		select {
		case <-ctx.Done():
			return response, ctx.Err()
		default:
			response, err = r.at.WriteStringContext(ctxAt, cmd)
			r.lastError = err

			fmt.Printf("CMD: %s, ERR: %v, ok: %q error: %q, done: %v, resp=%q\n",
				cmd, err, reOk.String(), reErr.String(), done, response)
		}
	}

	return response, r.lastError
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
