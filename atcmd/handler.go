package atcmd

import (
	"bytes"
	"container/list"
	"context"
	"errors"
	"io"
	"regexp"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ResponseFunc handle response from device.
// It should return true if expected response is valid.
// If the response is error, it will return error
type ResponseFunc func(data *SerialData, err error)
type ErrorFunc func(err error, done chan<- bool)

// list of constant
const (
	CR = 0x0D
	LF = 0x0A
)

const (
	OkPattern  = "OK(.*)\r\n"
	ErrPattern = "ERROR:(.*)\r\n"
)

// known data
var (
	CRLF        = []byte{CR, LF}
	RegexpOk    = regexp.MustCompile(OkPattern)
	RegexpError = regexp.MustCompile(ErrPattern)
)

// Handler
type Handler struct {
	sync.Mutex
	dev        io.ReadWriter
	rq         time.Duration
	fnResp     ResponseFunc
	fnError    ErrorFunc
	serialData *list.List
	cancel     context.CancelFunc
}

// SerialData stores information about serial data
type SerialData struct {
	At       time.Time
	ID       uuid.UUID
	Cmd      []byte
	Response []byte
	buf      *bytes.Buffer
	fn       ResponseFunc
}

// Copy serial data
func (sd *SerialData) Copy() *SerialData {
	stream := make([]byte, sd.buf.Len())
	copy(stream, sd.buf.Bytes())
	d := SerialData{
		At:       sd.At,
		ID:       sd.ID,
		Cmd:      sd.Cmd,
		Response: stream,
	}
	return &d
}

// NewHandler creates AT+Command Handler.
// rq: read quiescence time, i.e. delay between consecutive read
func NewHandler(dev io.ReadWriter, rqDuration time.Duration, onResp ResponseFunc, onErr ErrorFunc) *Handler {
	// create handler
	h := Handler{
		dev:        dev,
		rq:         rqDuration,
		serialData: list.New(),
		fnResp:     onResp,
		fnError:    onErr,
	}
	return &h
}

// NewDefaultHandler return AT command with default parameters
func NewDefaultHandler(dev io.ReadWriter) *Handler {
	return NewHandler(dev, 10*time.Millisecond, nil, nil)
}

// RunContext handler
func (h *Handler) RunContext(ctx context.Context) {
	// read loop with cancellable context
	ctx, cancel := context.WithCancel(ctx)
	h.cancel = cancel
	go h.readAsyncContext(ctx)

}

// Run read loop
func (h *Handler) Run() {
	h.RunContext(context.TODO())
}

// Close handler
func (h *Handler) Close() error {
	if h.cancel != nil {
		h.cancel()
	}
	return nil
}

// OnResponse Handler
func (h *Handler) OnResponse(fn ResponseFunc) *Handler {
	h.fnResp = fn
	return h
}

func (h *Handler) OnError(fn ErrorFunc) *Handler {
	h.fnError = fn
	return h
}

// ReadQuiescence set time delay between consecutive read
func (h *Handler) ReadQuiescence(delay time.Duration) *Handler {
	h.rq = delay
	return h
}

// SendContext send command without waiting response
func (h *Handler) SendContext(ctx context.Context, cmd []byte) error {
	_, _, err := h.writeCommandContext(ctx, cmd, nil)
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
	_, n, err := h.WriteContext(context.TODO(), cmd, nil)
	return n, err
}

// WriteContext write command with given context
func (h *Handler) WriteContext(ctx context.Context, cmd []byte, fn ResponseFunc) (uuid.UUID, int, error) {
	return h.writeCommandContext(ctx, cmd, fn)
}

// WriteString to the device
func (h *Handler) WriteString(cmd string, fn ResponseFunc) (uuid.UUID, error) {
	return h.WriteStringContext(context.TODO(), cmd, fn)
}

func (h *Handler) WriteStringContext(ctx context.Context, cmd string, fn ResponseFunc) (uuid.UUID, error) {
	id, _, err := h.writeCommandContext(ctx, []byte(cmd), fn)
	return id, err
}

func (h *Handler) writeCommandContext(ctx context.Context, data []byte, fn ResponseFunc) (uuid.UUID, int, error) {
	// send command
	nw, err := h.dev.Write(data)
	if err != nil {
		return uuid.Nil, nw, err
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
		n, err := h.dev.Write(CRLF)
		if err != nil {
			return uuid.Nil, nw, err
		}
		nw += n
	}

	// set command
	id := h.setCommand(data, fn)
	return id, nw, nil
}

// Reset content
func (h *Handler) Reset() {
	h.Lock()
	defer h.Unlock()
	h.serialData.Init()
}

/*
// ResponseBytes return reponse in byte array.
// The []byte is only valid until next modification.
func (h *Handler) ResponseBytes() []byte {
	return h.bufResponse.Bytes()
}

// ResponseBuffer return buffer that stores response bytes.
func (h *Handler) ResponseBuffer() *bytes.Buffer {
	return &h.bufResponse
}
*/

/*
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
func (h *Handler) DiscardIncoming() ([]byte, error) {
	return h.DiscardIncomingContext(context.TODO())
}

// DiscardContext discards device content
func (h *Handler) DiscardIncomingContext(ctx context.Context) ([]byte, error) {
	err := h.readDiscardContext(ctx, &h.bufDischard)
	return h.bufDischard.Bytes(), err
}

func (h *Handler) readDiscardContext(ctx context.Context, buf *bytes.Buffer) error {
	buf.Reset()
	for {
		select {
		case <-ctx.Done():
			return nil
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
*/

/*
func (h *Handler) readResponseContext(ctx context.Context, buf *bytes.Buffer) error {
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
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			}

			// check if response is set
			if h.fnResp != nil {
				ok, err := h.fnResp(buf.Bytes())
				if err != nil {
					return err
				}

				if ok {
					return nil
				}
			} else if err != nil && errors.Is(err, io.EOF) {
				return nil
			}

			// wait a moment
			time.Sleep(h.rq)
		}
	}
}
*/

func (h *Handler) readAsyncContext(ctx context.Context) error {
	chunk := make([]byte, 1024)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Read from device and save to buffer
			n, err := h.dev.Read(chunk)
			if n > 0 {
				if errors.Is(err, io.EOF) {
					err = nil
				}
				h.setResponseAsync(chunk[:n], err)
			}

			// check error
			if err != nil && !errors.Is(err, io.EOF) {
				if h.fnError != nil {
					done := make(chan bool, 1)
					go h.fnError(err, done)
					v := <-done
					if v {
						return err
					}
				}
			}

			// wait a moment
			time.Sleep(h.rq)
		}
	}
}

// set command called when newly added
func (h *Handler) setCommand(cmd []byte, fn ResponseFunc) uuid.UUID {
	ser := SerialData{
		At:  time.Now(),
		Cmd: cmd,
		ID:  uuid.New(),
		buf: &bytes.Buffer{},
		fn:  fn,
	}
	h.Lock()
	h.serialData.PushBack(&ser)
	h.Unlock()

	return ser.ID
}

func (h *Handler) setResponseAsync(resp []byte, err error) {
	h.Lock()
	defer h.Unlock()
	if elem := h.serialData.Back(); elem != nil {
		if elem.Value != nil {
			if ser, ok := elem.Value.(*SerialData); ok {
				ser.buf.Write(resp)

				// notify command
				if ser.fn != nil || h.fnResp != nil {
					d := ser.Copy()
					if ser.fn != nil {
						go ser.fn(d, err)
					}
					if h.fnResp != nil {
						go h.fnResp(d, err)
					}
				}
			}
		}
	} else {
		// create new serial data
		ser := SerialData{
			At:  time.Now(),
			buf: &bytes.Buffer{},
		}
		ser.buf.Write(resp)
		h.serialData.PushBack(&ser)
	}
}

// TakeResponse from buffer
func (h *Handler) TakeResponse(id uuid.UUID) *SerialData {
	h.Lock()
	defer h.Unlock()

	elem := h.serialData.Back()
	for elem != nil {
		if ser, ok := elem.Value.(*SerialData); ok {
			h.serialData.Remove(elem)
			ser.Response = ser.buf.Bytes()
			return ser
		}
		elem = elem.Prev()
	}
	return nil
}
