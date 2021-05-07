package rak

import "github.com/ipsusila/opt"

// Config for lora device
type Config struct {
	CommTimeout opt.Duration `json:"commTimeout"`
}
