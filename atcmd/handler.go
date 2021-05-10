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
type ResponseFunc func(data *SerialData, err error)
type ErrorFunc func(err error, done chan<- bool)

// list of constant
const (
	CR = 0x0D
	LF = 0x0A
)

const (
	OkPattern  = "OK(.*)\\r\\n"
	ErrPattern = "ERROR:(.*)\\r\\n"
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
	done       chan bool
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
		done:       make(chan bool, 1),
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

// Write command to device
func (h *Handler) Write(cmd []byte) (int, error) {
	_, n, err := h.WriteBytes(cmd, nil)
	return n, err
}

// WriteBytes write command with given context
func (h *Handler) WriteBytes(cmd []byte, fn ResponseFunc) (uuid.UUID, int, error) {
	return h.writeCommand(cmd, fn)
}

func (h *Handler) WriteString(cmd string, fn ResponseFunc) (uuid.UUID, int, error) {
	return h.writeCommand([]byte(cmd), fn)
}

func (h *Handler) writeCommand(data []byte, fn ResponseFunc) (uuid.UUID, int, error) {
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

func (h *Handler) readAsyncContext(ctx context.Context) error {
	defer close(h.done)
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
		if h.fnResp != nil {
			d := ser.Copy()
			go h.fnResp(d, err)
		}
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

// ResponseBytes read all data
func (h *Handler) ResponseBytes() []byte {
	h.Lock()
	defer h.Unlock()
	resp := []byte{}

	elem := h.serialData.Front()
	for elem != nil {
		if elem.Value != nil {
			if ser, ok := elem.Value.(*SerialData); ok {
				resp = append(resp, ser.buf.Bytes()...)
			}
		}
	}

	return resp
}

// IsRunning return true if read loop running
func (h *Handler) IsRunning() bool {
	select {
	case <-h.done:
		return false
	default:
		return true
	}
}

// WaitDone wait until done
func (h *Handler) WaitDoneContext(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-h.done:
		return nil
	}
}

// WaitDone wait until read loop is closed
func (h *Handler) WaitDone() error {
	return h.WaitDoneContext(context.TODO())
}
