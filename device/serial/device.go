package serial

import (
	"io"
	"time"

	"github.com/ipsusila/opt"
	serdev "github.com/tarm/serial"
)

// Device device
type Device interface {
	io.ReadWriteCloser
	Flush() error
}

type Config struct {
	Port        string       `json:"port"`
	Baudrate    int          `json:"baudrate"`
	ReadTimeout opt.Duration `json:"readTimeout"`
	Size        int          `json:"size"`
	Parity      string       `json:"parity"`
	StopBits    string       `json:"stopBits"`
}

func (c *Config) toSerialConfig() *serdev.Config {
	serCfg := serdev.Config{
		Name:        c.Port,
		Baud:        c.Baudrate,
		ReadTimeout: c.ReadTimeout.Duration,
		Size:        byte(c.Size),
	}
	/*
		ParityNone  Parity = 'N'
		ParityOdd   Parity = 'O'
		ParityEven  Parity = 'E'
		ParityMark  Parity = 'M' // parity bit is always 1
		ParitySpace Parity = 'S' // parity bit is always 0
	*/
	switch c.Parity {
	case "N":
		serCfg.Parity = serdev.ParityNone
	case "O":
		serCfg.Parity = serdev.ParityOdd
	case "E":
		serCfg.Parity = serdev.ParityEven
	case "M":
		serCfg.Parity = serdev.ParityMark
	case "S":
		serCfg.Parity = serdev.ParitySpace
	}

	switch c.StopBits {
	case "1":
		serCfg.StopBits = serdev.Stop1
	case "1.5":
		serCfg.StopBits = serdev.Stop1Half
	case "2":
		serCfg.StopBits = serdev.Stop2
	}

	return &serCfg
}

func New(conf *Config) (Device, error) {
	serCfg := conf.toSerialConfig()
	return serdev.OpenPort(serCfg)
}
func NewConfig(confFile, section string) (Device, error) {
	op, err := opt.FromFile(confFile, opt.FormatAuto)
	if err != nil {
		return nil, err
	}
	serOp := op.Get(section)
	conf := Config{
		Baudrate:    115200,
		ReadTimeout: opt.Duration{Duration: 1 * time.Second},
		Size:        8,
		Parity:      "N",
		StopBits:    "1",
	}
	if err := serOp.AsStruct(&conf); err != nil {
		return nil, err
	}

	return New(&conf)
}
