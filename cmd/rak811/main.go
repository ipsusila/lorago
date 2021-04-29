package main

import (
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

	response, err := r811.SendCommand(*vCommand, *vOk, *vErr, time.Duration(*vTimeout)*time.Second)
	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
	} else {
		fmt.Printf("==RESPONSE==\n%s\n", response)
		fmt.Printf("==RAW DATA==\n%q\n", response)
	}
}
