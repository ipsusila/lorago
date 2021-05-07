package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/ipsusila/lorago/device/rak"
)

func main() {
	vConfig := flag.String("config", "config.hjson", "Configuration file")
	vCommand := flag.String("command", "at+version", "AT command")
	vOk := flag.String("ok", "", "REGEX for OK response")
	vErr := flag.String("error", "", "REGEXP for ERROR response")
	vTimeout := flag.Int("timeout", 5, "Timeout in seconds")
	flag.Parse()

	r811, err := rak.NewRak811(*vConfig)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer r811.Close()

	// do initialization
	if err := r811.InitializeContext(context.Background()); err != nil {
		fmt.Println("Initialization error:", err)
		return
	}

	if *vCommand != "" {
		tmOut := time.Duration(*vTimeout) * time.Second
		ctx, cancel := context.WithTimeout(context.Background(), tmOut)
		defer cancel()

		response, err := r811.SendCommandContext(ctx, *vCommand, *vOk, *vErr, tmOut)
		if err != nil {
			fmt.Printf("ERROR: %v\n", err)
		} else {
			fmt.Printf("==RESPONSE==\n%s\n", response)
			fmt.Printf("==RAW DATA==\n%q\n", response)
		}
	}

	/*
		type fnRak func() (string, error)
		funcs := []fnRak{
			r811.FirmwareVersion,
			r811.AtHelp,
			r811.DeviceStatus,
			r811.LoraStatus,
		}
		for _, fn := range funcs {
			resp, err := fn()
			if err != nil {
				fmt.Printf("ERROR: %v\n", err)
			} else {
				fmt.Printf("==RESPONSE==\n%s\n", resp)
			}
		}
	*/

	// join
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	resp, err := r811.LoraJoinContext(ctx)
	fmt.Println("=== JOIN ===")
	fmt.Println(resp)
}
