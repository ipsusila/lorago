package rak

import (
	"bytes"
	"fmt"
	"regexp"
	"time"

	"github.com/ipsusila/lorago/atcmd"
	"github.com/ipsusila/lorago/device/serial"
)

const (
	SectionDevice = "dev"
	SectionLoRa   = "lora"
)

// wake up constant
var WakeUp = []byte("Wake Up.\r\n")

type Rak811 struct {
	dev        serial.Device
	at         *atcmd.Handler
	lastError  error
	maxTimeout time.Duration
}

// TODO: configuration

// NewRak811 create RAK-811 LoRa device connection
func NewRak811(confFile string) (*Rak811, error) {
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
		dev:        dev,
		at:         at,
		maxTimeout: 60 * time.Second,
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

func (r *Rak811) SendCommand(cmd string, okPattern, errPattern string, maxWait time.Duration) (string, error) {
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
	return r.sendCommand(cmd, reOk, reErr, maxWait)
}

func (r *Rak811) sendCommand(cmd string, reOk, reErr *regexp.Regexp, maxWait time.Duration) (string, error) {
	done := false
	fnResp := func(data []byte) (bool, error) {
		if bytes.Compare(data, WakeUp) == 0 {
			return true, nil
		} else if reOk.Match(data) || reErr.Match(data) {
			done = true
			return true, nil
		}
		return false, nil
	}

	// setup command
	var err error
	var response string
	r.at.OnResponse(fnResp)
	timeUntil := time.Now().Add(maxWait)
	for !done {
		now := time.Now()
		if now.After(timeUntil) {
			break
		}
		response, err = r.at.WriteString(cmd)
		r.lastError = err

		fmt.Printf("CMD: %s, ERR: %v, ok: %s, error: %s, done: %v\n",
			cmd, err, reOk.String(), reErr.String(), done)
	}

	return response, r.lastError
}

// FirmwareVersion return firmware version
func (r *Rak811) FirmwareVersion() (string, error) {
	// TODO: configure individual max timeout
	return r.sendCommand(atVersion, atcmd.RegexpOk, atcmd.RegexpError, r.maxTimeout)
}
