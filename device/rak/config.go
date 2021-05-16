package rak

import (
	"io"
	"io/ioutil"
	"os"
	"strings"

	"github.com/ipsusila/opt"
)

// Config for lora device
type Config struct {
	DefaultTimeout  opt.Duration `json:"defaultTimeout"`
	ResponseOutput  string       `json:"responseOutput"`
	Verbose         bool         `json:"verbose"`
	InitCommands    []*Command   `json:"initCommands"`
	ConfirmPattern  string       `json:"confirmPattern"`
	DownLinkPattern string       `json:"downLinkPattern"`
	LoRaChannel     int          `json:"loraChannel"`
}

// ResponseWriter return writer for the output
func (c *Config) ResponseWriter() (io.Writer, io.Closer, error) {
	output := strings.ToLower(c.ResponseOutput)
	switch output {
	case "stdout":
		return os.Stdout, nil, nil
	case "stderr":
		return os.Stderr, nil, nil
	case "null":
		return ioutil.Discard, nil, nil
	default:
		fd, err := os.Create(c.ResponseOutput)
		if err != nil {
			return nil, nil, err
		}
		return fd, fd, nil
	}
}
