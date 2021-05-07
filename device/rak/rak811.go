package rak

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/ipsusila/errutil"
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
	outCloser io.Closer
	outWriter io.Writer
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
	ra.conf.DefaultTimeout.Duration = 10 * time.Second
	if err := op.AsStruct(&ra.conf); err != nil {
		return nil, err
	}

	// get output
	wr, cl, err := ra.conf.ResponseWriter()
	ra.outWriter = wr
	ra.outCloser = cl
	if err != nil {
		return nil, err
	}

	// run loop
	at.Run()

	return &ra, nil
}

// Close connection to device
func (r *Rak811) Close() error {
	errs := errutil.New()
	if r.at != nil {
		errs.Append(r.at.Close())
		errs.Append(r.at.WaitDone())
		r.at = nil
	}
	if dev := r.dev; dev != nil {
		r.dev = nil
		errs.Append(dev.Close())
	}

	// closer
	if r.outCloser != nil {
		errs.Append(r.outCloser.Close())
	}
	return errs.AsError()
}

// Initialize run init commands
func (r *Rak811) InitializeContext(ctx context.Context) error {
	for _, c := range r.conf.InitCommands {
		_, err := r.sendCommandContext(ctx, c)
		if err != nil {
			return err
		}
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
	if r.conf.Verbose && r.outWriter != nil {
		fmt.Fprintf(r.outWriter, "@%s CMD: %q, DATA: %q\n",
			data.At.Format(time.RFC3339),
			string(data.Cmd),
			string(data.Response))
	}
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
	c := Command{
		AT:         cmd,
		RegexOK:    okPattern,
		RegexError: errPattern,
		Timeout:    opt.Duration{Duration: maxWait},
	}
	return r.sendCommandContext(ctx, &c)
}

func (r *Rak811) sendCommandContext(ctx context.Context, cmd *Command) (string, error) {
	done := make(chan bool, 1)
	resend := make(chan bool, 1)
	quit := make(chan bool)

	reOk, err := cmd.ExpresionOK()
	if err != nil {
		return "", err
	}

	reErr, err := cmd.ExpresionError()
	if err != nil {
		return "", err
	}

	reWakeup, err := cmd.ExpresionWakeup()
	if err != nil {
		return "", err
	}
	fnResp := func(data *atcmd.SerialData, err error) {
		if err != nil {
			close(quit)
		} else if reWakeup.Match(data.Response) {
			resend <- true
		} else if reOk.Match(data.Response) || reErr.Match(data.Response) {
			close(done)
		}

	}

	// setup context
	if cmd.Timeout.Duration > 0 {
		c, cancel := context.WithTimeout(ctx, cmd.Timeout.Duration)
		defer cancel()
		ctx = c
	}

	// setup command
	var response string
	id, _, err := r.at.WriteString(cmd.AT, fnResp)
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
			id, _, err = r.at.WriteString(cmd.AT, fnResp)
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
	return r.sendCommandContext(ctx, NewCommand(atVersion, r.conf.DefaultTimeout.Duration))
}

// AtHelp return help
func (r *Rak811) AtHelp() (string, error) {
	return r.AtHelpContext(context.TODO())
}

// AtHelpContext return help
func (r *Rak811) AtHelpContext(ctx context.Context) (string, error) {
	return r.sendListComandContext(ctx, atHelp)
}

func (r *Rak811) DeviceStatus() (string, error) {
	return r.DeviceStatusContext(context.TODO())
}

func (r *Rak811) DeviceStatusContext(ctx context.Context) (string, error) {
	return r.sendListComandContext(ctx, atDeviceStatus)
}

func (r *Rak811) LoraStatus() (string, error) {
	return r.LoraStatusContext(context.TODO())
}

func (r *Rak811) LoraStatusContext(ctx context.Context) (string, error) {
	return r.sendListComandContext(ctx, atLoraStatus)
}

func (r *Rak811) LoraJoinContext(ctx context.Context) (string, error) {
	return r.sendCommandContext(ctx, &Command{AT: atLoraJoin})
}

func (r *Rak811) sendListComandContext(ctx context.Context, cmd string) (string, error) {
	okPattern := "(?s)OK(.*)List End(.*)\\*\r\n"
	c := Command{
		AT:      cmd,
		Timeout: r.conf.DefaultTimeout,
		RegexOK: okPattern,
	}
	return r.sendCommandContext(ctx, &c)
}

// SendDataContext sends data through LoRa
func (r *Rak811) SendDataContext(ctx context.Context, channel int, data []byte) ([]byte, error) {
	// TODO, OK: pattern, ERROR: Error pattern
	at := fmt.Sprintf(atLoraSend, channel, data)
	res, err := r.sendCommandContext(ctx, NewCommand(at, r.conf.DefaultTimeout.Duration))
	if err != nil {
		return nil, err
	}
	return []byte(res), nil
}

// SendData sends data through LoRa
func (r *Rak811) SendData(ctx context.Context, channel int, data []byte) ([]byte, error) {
	return r.SendDataContext(context.TODO(), channel, data)
}
