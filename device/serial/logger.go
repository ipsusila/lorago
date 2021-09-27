package serial

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"strings"
	"time"

	util "github.com/ipsusila/go-util"
	"github.com/ipsusila/opt"
)

type Syncher interface {
	Sync() error
}

type Logger struct {
	done     chan bool
	dev      Device
	w        io.Writer
	c        io.Closer
	s        Syncher
	confFile string

	Interval opt.Duration `json:"interval"`
	Output   string       `json:"output"`
	Verbose  bool         `json:"verbose"`
}

// NewLogger create serial logger
func NewLogger(confFile string) *Logger {
	return &Logger{
		confFile: confFile,
	}
}

func (l *Logger) Open() error {
	if l.dev != nil {
		return nil
	}

	// create new device
	dev, err := NewConfig(l.confFile, "dev")
	if err != nil {
		return err
	}

	op, err := opt.FromFile(l.confFile, opt.FormatAuto)
	if err != nil {
		return err
	}
	lgrOp := op.Get("logger")
	l.Interval = opt.Duration{Duration: 100 * time.Millisecond}
	l.Output = "output.csv"
	if err := lgrOp.AsStruct(l); err != nil {
		return err
	}

	l.dev = dev
	l.done = make(chan bool, 1)

	outStr := strings.ToLower(strings.TrimSpace(l.Output))
	switch outStr {
	case "stdout":
		l.w = os.Stdout
		l.c = nil
	case "stderr":
		l.w = os.Stderr
		l.c = nil
	case "":
		return errors.New("output is not specified")
	default:
		fd, err := os.Create(l.Output)
		if err != nil {
			return err
		}
		l.w = fd
		l.c = fd
		l.s = fd
	}

	if l.Verbose {
		log.Printf("Logger configuration:\n%s\n", util.PrettyColorStr(l))
	}

	return nil
}

// Stop logger
func (l *Logger) Stop() error {
	return l.Close()
}

func (l *Logger) Close() error {
	if l.done != nil {
		<-l.done
	}
	l.done = nil
	l.dev = nil
	l.c = nil
	l.w = nil
	l.s = nil

	return nil
}

func (l *Logger) ReadContext(ctx context.Context) error {
	defer close(l.done)
	defer l.dev.Close()
	defer l.c.Close()

	startedAt := time.Now()
	chunk := make([]byte, 1024)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Read from device and save to buffer
			n, err := l.dev.Read(chunk)
			if n > 0 {
				if errors.Is(err, io.EOF) {
					err = nil
				}
				if _, errw := l.w.Write(chunk[:n]); err != nil {
					log.Println("Serial port data write error:", errw)
				} else if l.s != nil {
					l.s.Sync()
				}

				if l.Verbose {
					log.Printf("Read data count: %d byte(s), elapsed: %v\n", n, time.Since(startedAt))
				}
			}

			// check error
			if err != nil && !errors.Is(err, io.EOF) {
				log.Println("Serial port read error:", err)
			}

			// wait a moment
			time.Sleep(l.Interval.Duration)
		}
	}
}
