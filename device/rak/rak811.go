package rak

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ipsusila/errutil"
	"github.com/ipsusila/lorago/atcmd"
	"github.com/ipsusila/lorago/device/serial"
	"github.com/ipsusila/opt"
)

const (
	SectionDevice = "dev"
	SectionLoRa   = "lora"
)

const (
	StatusOK    = "OK"
	StatusError = "ERROR"
	StatusNone  = ""
)

const (
	LoRaConfirmPattern  = "at\\+recv=(.*):(.*)\\r\\n"
	LoRaDownLinkPattern = "at\\+recv=(.*):(.*)\\r\\n"
)

// wake up constant
var WakeUp = []byte("Wake up.\r\n")

type Rak811 struct {
	dev            serial.Device
	at             *atcmd.Handler
	lastError      error
	conf           Config
	outCloser      io.Closer
	outWriter      io.Writer
	reDownLinkExpr *regexp.Regexp

	loraStatus       *LoRaStatus
	incomingDataList []*IncomingData
}

type IncomingData struct {
	At   time.Time
	Data []byte
}

// LoraSt
type LoRaStatus struct {
	At                    time.Time    `json:"at"`
	WorkMode              string       `json:"workMode"`
	Region                string       `json:"region"`
	SendInterval          opt.Duration `json:"sendInterval"`
	AutoSendStatus        bool         `json:"autoSendStatus"`
	JoinMode              string       `json:"joinMode"`
	DevEUI                string       `json:"devEUI"`
	AppEUI                string       `json:"appEUI"`
	AppKey                string       `json:"appKey"`
	Class                 string       `json:"class"`
	JoinedNetwork         bool         `json:"joinedNetwork"`
	IsConfirm             bool         `json:"isConfirm"`
	AdrEnable             bool         `json:"adrEnable"`
	EnableRepeaterSupport bool         `json:"enableRepeaterSupport"`
	Rx2ChannelFrequency   int64        `json:"rx2ChannelFrequency"`
	Rx2ChannelDR          int          `json:"rx2ChannelDR"`
	RxWindowDuration      opt.Duration `json:"rxWindowDuration"`
	ReceiveDelay1         opt.Duration `json:"receiveDelay1"`
	ReceiveDelay2         opt.Duration `json:"receiveDelay2"`
	JoinAcceptDelay1      opt.Duration `json:"joinAcceptDelay1"`
	JoinAcceptDelay2      opt.Duration `json:"joinAcceptDelay2"`
	CurrentDataRate       int          `json:"currentDataRate"`
	PrimevalDataRate      int          `json:"privevalDataRate"`
	ChannelsTxPower       int          `json:"channelsTxPower"`
	UpLinkCounter         int          `json:"upLinkCounter"`
	DownLinkCounter       int          `json:"downLinkCounter"`
}

// NewRak811 create RAK-811 LoRa device connection
func NewRak811(confFile string) (*Rak811, error) {
	// configuration
	op, err := opt.FromFile(confFile, opt.FormatAuto)
	if err != nil {
		return nil, err
	}

	// serial command
	dev, err := serial.NewConfig(confFile, SectionDevice)
	if err != nil {
		return nil, err
	}

	// create command handler
	at := atcmd.NewDefaultHandler(dev)

	// create new Rak811 device
	ra := Rak811{
		dev:        dev,
		at:         at,
		loraStatus: &LoRaStatus{},
	}

	// setup handler
	at.OnError(ra.onError)
	at.OnResponse(ra.onResponse)

	// Default comm timeout
	ra.conf.DefaultTimeout.Duration = 10 * time.Second
	ra.conf.ConfirmPattern = LoRaConfirmPattern
	ra.conf.DownLinkPattern = LoRaDownLinkPattern
	if err := op.AsStruct(&ra.conf); err != nil {
		return nil, err
	}

	// create receive pattern
	ra.reDownLinkExpr = regexp.MustCompile(ra.conf.DownLinkPattern)

	// get output
	wr, cl, err := ra.conf.ResponseWriter()
	ra.outWriter = wr
	ra.outCloser = cl
	if err != nil {
		return nil, err
	}

	// run loop
	at.Run()

	return &ra, nil
}

// Close connection to device
func (r *Rak811) Close() error {
	errs := errutil.New()
	if r.at != nil {
		errs.Append(r.at.Close())
		errs.Append(r.at.WaitDone())
		r.at = nil
	}
	if dev := r.dev; dev != nil {
		r.dev = nil
		errs.Append(dev.Close())
	}

	// closer
	if r.outCloser != nil {
		errs.Append(r.outCloser.Close())
	}
	return errs.AsError()
}

// Initialize run init commands
func (r *Rak811) InitializeContext(ctx context.Context) (string, error) {
	status := StatusNone
	var err error
	for _, c := range r.conf.InitCommands {
		_, status, err = r.sendCommandContext(ctx, c)
		if err != nil {
			return status, err
		}
	}
	return status, nil
}

// OnError handler
func (r *Rak811) onError(err error, done chan<- bool) {
	log.Printf("Device error: %v\n", err)
	r.Close()
	done <- true
}

//OnResponse function
func (r *Rak811) onResponse(data *atcmd.SerialData, err error) {
	if r.conf.Verbose && r.outWriter != nil {
		fmt.Fprintf(r.outWriter, "@%s CMD: %q, DATA: %q\n",
			data.At.Format(time.RFC3339),
			string(data.Cmd),
			string(data.Response))
	}

	// is it a data?
	// 2
	match := r.reDownLinkExpr.FindSubmatch(data.Response)
	if n := len(match); n > 0 {
		if r.conf.Verbose && r.outWriter != nil {
			fmt.Fprintf(r.outWriter, "DATA>> %s\n", string(data.Response))
		}
		if nd := len(match); nd == 3 && len(match[2]) > 0 {
			buf := make([]byte, len(match[2])/2)
			nw, err := hex.Decode(buf, match[2])
			if err == nil {
				incomingData := &IncomingData{
					At:   time.Now(),
					Data: buf[:nw],
				}
				r.incomingDataList = append(r.incomingDataList, incomingData)
				if r.conf.Verbose && r.outWriter != nil {
					fmt.Fprintf(r.outWriter, "DATA: %02X\n", buf)
				}
			}
		}
	}
}

func (r *Rak811) parseLoraStatus(status string, ls *LoRaStatus) *LoRaStatus {
	ls.At = time.Now()
	scanner := bufio.NewScanner(strings.NewReader(status))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "RX2_CHANNEL_FREQUENCY") {
			items := strings.Split(line, ",")
			for _, item := range items {
				fields := strings.Split(item, ":")
				if len(fields) >= 2 {
					key := strings.TrimSpace(fields[0])
					value := strings.TrimSpace(fields[1])
					switch key {
					case "RX2_CHANNEL_FREQUENCY":
						if v, err := strconv.ParseInt(value, 10, 64); err == nil {
							ls.Rx2ChannelFrequency = v
						}
					case "RX2_CHANNEL_DR":
						if v, err := strconv.Atoi(value); err == nil {
							ls.Rx2ChannelDR = v
						}
					}
				}
			}
			continue
		}

		fields := strings.Split(line, ":")
		if len(fields) >= 2 {
			key := strings.TrimSpace(fields[0])
			value := strings.TrimSpace(fields[1])
			switch key {
			case "Work Mode":
				ls.WorkMode = value
			case "Region":
				ls.Region = value
			case "Send_interval":
				if d, err := time.ParseDuration(value); err == nil {
					ls.SendInterval = opt.Duration{Duration: d}
				}
			case "Auto send status":
				if v, err := strconv.ParseBool(value); err == nil {
					ls.AutoSendStatus = v
				}
			case "Join_mode":
				ls.JoinMode = value
			case "DevEui":
				ls.DevEUI = value
			case "AppEui":
				ls.AppEUI = value
			case "AppKey":
				ls.AppKey = value
			case "Class":
				ls.Class = value
			case "Joined Network":
				if v, err := strconv.ParseBool(value); err == nil {
					ls.JoinedNetwork = v
				}
			case "IsConfirm":
				if v, err := strconv.ParseBool(value); err == nil {
					ls.IsConfirm = v
				}
			case "AdrEnable":
				if v, err := strconv.ParseBool(value); err == nil {
					ls.AdrEnable = v
				}
			case "EnableRepeaterSupport":
				if v, err := strconv.ParseBool(value); err == nil {
					ls.EnableRepeaterSupport = v
				}
			case "RX_WINDOW_DURATION":
				if d, err := time.ParseDuration(value); err == nil {
					ls.RxWindowDuration = opt.Duration{Duration: d}
				}
			case "RECEIVE_DELAY_1":
				if d, err := time.ParseDuration(value); err == nil {
					ls.ReceiveDelay1 = opt.Duration{Duration: d}
				}
			case "RECEIVE_DELAY_2":
				if d, err := time.ParseDuration(value); err == nil {
					ls.ReceiveDelay2 = opt.Duration{Duration: d}
				}
			case "JOIN_ACCEPT_DELAY_1":
				if d, err := time.ParseDuration(value); err == nil {
					ls.JoinAcceptDelay1 = opt.Duration{Duration: d}
				}
			case "JOIN_ACCEPT_DELAY_2":
				if d, err := time.ParseDuration(value); err == nil {
					ls.JoinAcceptDelay2 = opt.Duration{Duration: d}
				}
			case "Current Datarate":
				if v, err := strconv.Atoi(value); err == nil {
					ls.CurrentDataRate = v
				}
			case "Primeval Datarate":
				if v, err := strconv.Atoi(value); err == nil {
					ls.PrimevalDataRate = v
				}
			case "ChannelsTxPower":
				if v, err := strconv.Atoi(value); err == nil {
					ls.ChannelsTxPower = v
				}
			case "UpLinkCounter":
				if v, err := strconv.Atoi(value); err == nil {
					ls.UpLinkCounter = v
				}
			case "DownLinkCounter":
				if v, err := strconv.Atoi(value); err == nil {
					ls.DownLinkCounter = v
				}
			}
		}
	}

	return ls
}

// LastError return latest error
func (r *Rak811) LastError() error {
	return r.lastError
}

// IsValid return true if device connection is not closed.
func (r *Rak811) IsValid() bool {
	return r.dev != nil && r.at != nil
}

func (r *Rak811) SendCommandContext(ctx context.Context, cmd string, okPattern, errPattern string, maxWait time.Duration) (string, string, error) {
	c := Command{
		AT:         cmd,
		RegexOK:    okPattern,
		RegexError: errPattern,
		Timeout:    opt.Duration{Duration: maxWait},
	}
	return r.sendCommandContext(ctx, &c)
}

func (r *Rak811) sendCommandContext(ctx context.Context, cmd *Command) (string, string, error) {
	done := make(chan bool, 1)
	resend := make(chan bool, 1)
	quit := make(chan bool)

	status := StatusNone
	reOk, err := cmd.ExpresionOK()
	if err != nil {
		return "", status, err
	}

	reErr, err := cmd.ExpresionError()
	if err != nil {
		return "", status, err
	}

	reWakeup, err := cmd.ExpresionWakeup()
	if err != nil {
		return "", status, err
	}

	fnResp := func(data *atcmd.SerialData, err error) {
		if err != nil {
			close(quit)
		} else if reWakeup.Match(data.Response) {
			resend <- true
		} else if reOk.Match(data.Response) {
			status = StatusOK
			close(done)
		} else if reErr.Match(data.Response) {
			status = StatusError
			close(done)
		}

	}

	// setup context
	if cmd.Timeout.Duration > 0 {
		c, cancel := context.WithTimeout(ctx, cmd.Timeout.Duration)
		defer cancel()
		ctx = c
	}

	// setup command
	var response string
	id, _, err := r.at.WriteString(cmd.AT, fnResp)
	if err != nil {
		return "", status, err
	}
	for {
		select {
		case <-ctx.Done():
			return response, status, ctx.Err()
		case <-done:
			data := r.at.TakeResponse(id)
			return r.commandResponse(cmd, data, status, nil)
		case <-resend:
			id, _, err = r.at.WriteString(cmd.AT, fnResp)
			if err != nil {
				return "", status, err
			}
		}
	}
}

func (r *Rak811) commandResponse(c *Command, data *atcmd.SerialData, status string, err error) (string, string, error) {
	at := strings.ToLower(c.AT)
	resp := string(data.Response)
	if at == atLoraStatus {
		r.parseLoraStatus(resp, r.loraStatus)
	}
	return resp, status, err
}

// FirmwareVersion return firmware version
func (r *Rak811) FirmwareVersion() (string, string, error) {
	return r.FirmwareVersionContext(context.TODO())
}

// FirmwareVersionContext check version
func (r *Rak811) FirmwareVersionContext(ctx context.Context) (string, string, error) {
	return r.sendCommandContext(ctx, NewCommand(atVersion, r.conf.DefaultTimeout.Duration))
}

// AtHelp return help
func (r *Rak811) AtHelp() (string, string, error) {
	return r.AtHelpContext(context.TODO())
}

// AtHelpContext return help
func (r *Rak811) AtHelpContext(ctx context.Context) (string, string, error) {
	return r.sendListComandContext(ctx, atHelp)
}

func (r *Rak811) DeviceStatus() (string, string, error) {
	return r.DeviceStatusContext(context.TODO())
}

func (r *Rak811) DeviceStatusContext(ctx context.Context) (string, string, error) {
	return r.sendListComandContext(ctx, atDeviceStatus)
}

func (r *Rak811) LoraStatus() (string, string, error) {
	return r.LoraStatusContext(context.TODO())
}

func (r *Rak811) LoraStatusContext(ctx context.Context) (string, string, error) {
	return r.sendListComandContext(ctx, atLoraStatus)
}

// LastLoraStatus return latest readed status
func (r *Rak811) LastLoraStatus() *LoRaStatus {
	return r.loraStatus
}

func (r *Rak811) LoraJoinContext(ctx context.Context) (string, string, error) {
	return r.sendCommandContext(ctx, &Command{AT: atLoraJoin})
}

func (r *Rak811) LoraSetDataRateContext(ctx context.Context, dr int) (string, string, error) {
	atDr := fmt.Sprintf(atLoraSetDataRate, dr)
	return r.sendCommandContext(ctx, NewCommand(atDr, r.conf.DefaultTimeout.Duration))
}

func (r *Rak811) sendListComandContext(ctx context.Context, cmd string) (string, string, error) {
	okPattern := "(?s)OK(.*)List End(.*)\\*\\r\\n"
	c := Command{
		AT:      cmd,
		Timeout: r.conf.DefaultTimeout,
		RegexOK: okPattern,
	}
	return r.sendCommandContext(ctx, &c)
}

// SendDataContext sends data through LoRa
func (r *Rak811) SendDataContext(ctx context.Context, channel int, data []byte) ([]byte, string, error) {
	// Incomming data: at+recv=2,-59,34,4:50515253
	// ch,RSSI,SNR?,channel:HEX
	at := fmt.Sprintf(atLoraSend, channel, data)
	rcvPattern := LoRaConfirmPattern
	if r.conf.ConfirmPattern != "" {
		rcvPattern = r.conf.ConfirmPattern
	}
	cmd := Command{
		AT:      at,
		Timeout: r.conf.DefaultTimeout,
		RegexOK: rcvPattern, //"at\\+recv=(.*)\\r\\n",
	}
	res, status, err := r.sendCommandContext(ctx, &cmd)
	if err != nil {
		return nil, status, err
	}
	return []byte(res), status, nil
}

// SendData sends data through LoRa
func (r *Rak811) SendData(ctx context.Context, channel int, data []byte) ([]byte, string, error) {
	return r.SendDataContext(context.TODO(), channel, data)
}
