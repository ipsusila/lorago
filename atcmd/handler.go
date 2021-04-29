package atcmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"regexp"
	"time"
)

// list of constant
const (
	CR = 0x0D
	LF = 0x0A
)

const (
	okPattern  = "OK(.*)\r\n"
	errPattern = "ERROR:(.*)\r\n"
)

// known data
var CRLF = []byte{CR, LF}

// Handler
type Handler struct {
	dev         io.ReadWriter
	rq          time.Duration
	chunk       []byte
	lastWritten int
	bufDischard bytes.Buffer
	bufResponse bytes.Buffer
	reOK        *regexp.Regexp
	reError     *regexp.Regexp
}

// NewHandler creates AT+Command Handler.
// rq: read quiescence time, i.e. delay between consecutive read
func NewHandler(dev io.ReadWriter, rqDuration time.Duration, reOK, reError string) (*Handler, error) {
	// OK pattern
	if reOK == "" {
		reOK = okPattern
	}
	okRe, err := regexp.Compile(reOK)
	if err != nil {
		return nil, err
	}

	// Error pattern
	if reError == "" {
		reError = errPattern
	}
	errRe, err := regexp.Compile(reError)
	if err != nil {
		return nil, err
	}

	// create handler
	h := Handler{
		dev:         dev,
		rq:          rqDuration,
		chunk:       make([]byte, 1024),
		lastWritten: 0,
		reOK:        okRe,
		reError:     errRe,
	}

	return &h, nil
}

// NewDefaultHandler return AT command with default parameters
func NewDefaultHandler(dev io.ReadWriter) (*Handler, error) {
	return NewHandler(dev, 10*time.Millisecond, okPattern, errPattern)
}

// SendContext send command without waiting response
func (h *Handler) SendContext(ctx context.Context, cmd []byte) error {
	_, err := h.writeCommandContext(ctx, cmd, false)
	return err
}

// Send command to device
func (h *Handler) Send(cmd []byte) error {
	return h.SendContext(context.TODO(), cmd)
}

// SendString command
func (h *Handler) SendString(cmd string) error {
	return h.Send([]byte(cmd))
}

// SendStringContext send command in string
func (h *Handler) SendStringContext(ctx context.Context, cmd string) error {
	return h.SendContext(ctx, []byte(cmd))
}

// Write command to device
func (h *Handler) Write(cmd []byte) (int, error) {
	err := h.WriteContext(context.TODO(), cmd)
	return h.lastWritten, err
}

// WriteContext write command with given context
func (h *Handler) WriteContext(ctx context.Context, cmd []byte) error {
	_, err := h.writeCommandContext(ctx, cmd, true)
	return err
}

// WriteString to the device
func (h *Handler) WriteString(cmd string) (string, error) {
	return h.WriteStringContext(context.TODO(), cmd)
}

func (h *Handler) WriteStringContext(ctx context.Context, cmd string) (string, error) {
	resp, err := h.writeCommandContext(ctx, []byte(cmd), true)
	return string(resp), err
}

func (h *Handler) writeCommandContext(ctx context.Context, data []byte, readResp bool) ([]byte, error) {
	// no bytes writen yet
	h.lastWritten = 0

	// dischard any data
	if _, err := h.DiscardContext(ctx); err != nil {
		return nil, err
	}

	// send command
	nw, err := h.dev.Write(data)
	h.lastWritten = nw
	if err != nil {
		return nil, err
	}

	// do we need to add CR and LF
	nd := len(data)
	addCRLF := false
	if nd < 2 {
		addCRLF = true
	} else {
		cr := data[nd-2]
		lf := data[nd-1]
		if cr != CR || lf != LF {
			addCRLF = true
		}
	}

	// write crlf
	if addCRLF {
		nw, err := h.dev.Write(CRLF)
		if err != nil {
			return nil, err
		}
		h.lastWritten += nw
	}

	if readResp {
		// read response
		err = h.readCheckContext(ctx, &h.bufResponse)
		return h.bufDischard.Bytes(), err
	}

	return nil, nil
}

// Reset content
func (h *Handler) Reset() {
	h.bufDischard.Reset()
	h.bufResponse.Reset()
}

// ResponseBytes return reponse in byte array.
// The []byte is only valid until next modification.
func (h *Handler) ResponseBytes() []byte {
	return h.bufResponse.Bytes()
}

// ResponseBuffer return buffer that stores response bytes.
func (h *Handler) ResponseBuffer() *bytes.Buffer {
	return &h.bufResponse
}

// DiscardedBytes return last discarded content.
// Returned bytes only valid until last modification.
func (h *Handler) DiscardedBytes() []byte {
	return h.bufDischard.Bytes()
}

// DiscardedBuffer return discarded buffer
func (h *Handler) DiscardedBuffer() *bytes.Buffer {
	return &h.bufDischard
}

// Discard bytes from device.
// Returned []byte only valid until next modification.
func (h *Handler) Discard() ([]byte, error) {
	return h.DiscardContext(context.TODO())
}

// DiscardContext discards device content
func (h *Handler) DiscardContext(ctx context.Context) ([]byte, error) {
	err := h.readContext(ctx, &h.bufDischard)
	return h.bufDischard.Bytes(), err
}

func (h *Handler) readContext(ctx context.Context, buf *bytes.Buffer) error {
	buf.Reset()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Read from device and save to buffer
			n, err := h.dev.Read(h.chunk)
			if n > 0 {
				buf.Write(h.chunk[:n])
			}

			// check error
			if err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}

			// wait a moment
			time.Sleep(h.rq)
		}
	}
}

func (h *Handler) readCheckContext(ctx context.Context, buf *bytes.Buffer) error {
	buf.Reset()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Read from device and save to buffer
			n, err := h.dev.Read(h.chunk)
			if n > 0 {
				buf.Write(h.chunk[:n])
			}

			// check error
			if err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}

			// response is OK or Error
			if h.IsSuccess() || h.IsError() {
				return nil
			}

			// wait a moment
			time.Sleep(h.rq)
		}
	}
}

// return true if command is success
func (h *Handler) IsSuccess() bool {
	resp := h.ResponseBytes()
	return h.reOK.Match(resp)
}

// return true if error
func (h *Handler) IsError() bool {
	resp := h.ResponseBytes()
	return h.reError.Match(resp)
}
