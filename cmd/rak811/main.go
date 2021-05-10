package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"time"

	"github.com/ipsusila/lorago/device/rak"
)

func main() {
	vConfig := flag.String("config", "config.hjson", "Configuration file")
	vCommand := flag.String("command", "", "AT command")
	vOk := flag.String("ok", "", "REGEX for OK response")
	vErr := flag.String("error", "", "REGEXP for ERROR response")
	vTimeout := flag.Int("timeout", 5, "Timeout in seconds")
	vData := flag.String("data", "", "Data to be send through LoRa")
	vChannel := flag.Int("channel", 3, "LoRa channel")
	flag.Parse()

	r811, err := rak.NewRak811(*vConfig)
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

	// 2. send custom command
	if *vCommand != "" {
		tmOut := time.Duration(*vTimeout) * time.Second
		ctx, cancel := context.WithTimeout(context.Background(), tmOut)
		defer cancel()

		response, status, err := r811.SendCommandContext(ctx, *vCommand, *vOk, *vErr, tmOut)
		if err != nil {
			fmt.Printf("ERROR: %v\n", err)
		} else {
			fmt.Printf("==RESPONSE== (%s)\n%s\n", status, response)
			fmt.Printf("==RAW DATA== (%s)\n%q\n", status, response)
		}
	}

	// 3. join?
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	ls := r811.LastLoraStatus()
	if !ls.JoinedNetwork {
		fmt.Println(" Joining network ....")
		resp, status, err := r811.LoraJoinContext(ctx)
		fmt.Println("=== JOIN ===, status:", status, ", error:", err)
		fmt.Println(resp)
	}

	// recheck lora status
	r811.LoraStatusContext(ctx)
	ls = r811.LastLoraStatus()
	if ls.JoinedNetwork && *vData != "" {
		for i := 0; i < 10; i++ {
			// Test sending data
			resp, status, err := r811.SendDataContext(ctx, *vChannel, []byte(*vData))
			fmt.Println(string(resp), status, err)
			time.Sleep(2 * time.Second)
		}
	}
}
