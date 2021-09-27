package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/ipsusila/lorago/device/serial"
)

func main() {
	vConfig := flag.String("config", "config.hjson", "Configuration file")
	flag.Parse()

	lgr := serial.NewLogger(*vConfig)
	if err := lgr.Open(); err != nil {
		log.Fatalln(err)
	}

	// start logging
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		if err := lgr.ReadContext(ctx); !errors.Is(err, context.Canceled) {
			log.Println("Read Serial error:", err)
		}
	}()

	// wait for signaled
	log.Println("Press CTRL+C to finish logging")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGINT)

	<-sig
	cancel()
	lgr.Close()
	log.Println("Data logger done!")
}
