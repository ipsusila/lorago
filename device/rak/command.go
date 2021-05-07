package rak

import (
	"regexp"
	"time"

	"github.com/ipsusila/lorago/atcmd"
	"github.com/ipsusila/opt"
)

// List of known at command.
// P2P config at+set_config=lorap2p:<frequency>:<spreadfact>:<bandwidth>:<codingrate>:<preamlen>:<power>
const (
	atVersion                  = "at+version"
	atHelp                     = "at+help"
	atDeviceRestart            = "at+set_config=device:restart"
	atDeviceSleep              = "at+set_config=device:sleep:1"
	atDeviceWakeup             = "at+set_config=device:sleep:0"
	atDeviceStatus             = "at+get_config=device:status"
	atUartSetBaud              = "at+set_config=device:uart:%d:%d"
	atUartSetMode              = "at+set_config=device:uart_mode:%d:%d"
	atUartSendData             = "at+send=uart:%d:%s"
	atDeviceSetGPIO            = "at+get_config=device:gpio:%d"
	atDeviceGetGPIO            = "at+set_config=device:gpio:%d:%d"
	atDeviceGetADC             = "at+get_config=device:adc:%d"
	atLoraJoin                 = "at+join"
	atLoraSend                 = "at+send=lora:%d:%02X"
	atLoraSetRegion            = "at+set_config=lora:region:%s"
	atLoraGetChannel           = "at+get_config=lora:channel"
	atLoraChannelOn            = "at+set_config=lora:ch_mask:%d:1"
	atLoraChannelOff           = "at+set_config=lora:ch_mask:%d:0"
	atLoraSetDevEUI            = "at+set_config=lora:dev_eui:%s"
	atLoraSetAppEUI            = "at+set_config=lora:app_eui:%s"
	atLoraSetAppKey            = "at+set_config=lora:app_key:%s"
	atLoraSetDevAddr           = "at+set_config=lora:dev_addr:%s"
	atLoraSetAppsKey           = "at+set_config=lora:apps_key:%s"
	atLoraSetNwksKey           = "at+set_config=lora:nwks_key:%s"
	atLoraEnableMulticast      = "at+set_config=lora:multicastenable:1"
	atLoraDisableMulticast     = "at+set_config=lora:multicastenable:0"
	atLoraSetMulticastDevAddr  = "at+set_config=lora:multicast_dev_addr:%s"
	atLoraSetMulticastAppsKey  = "at+set_config=lora:multicast_apps_key:%s"
	atLoraSetMulticastNwksKey  = "at+set_config=lora:multicast_nwks_key:%s"
	atLoraSetModeOTAA          = "at+set_config=lora:join_mode:0"
	atLoraSetModeABP           = "at+set_config=lora:join_mode:1"
	atLoraSetClassA            = "at+set_config=lora:class:0"
	atLoraSetClassB            = "at+set_config=lora:class:1"
	atLoraSetClassC            = "at+set_config=lora:class:2"
	atLoraMessageConfirm       = "at+set_config=lora:confirm:1"
	atLoraMessageUnconfirm     = "at+set_config=lora:confirm:0"
	atLoraSetDataRate          = "at+set_config=lora:dr:%d"
	atLoraSetTxPower           = "at+set_config=lora:tx_power:%d"
	atLoraADROn                = "at+set_config=lora:adr:1"
	atLoraADROff               = "at+set_config=lora:adr:0"
	atLoraStatus               = "at+get_config=lora:status"
	atLoraEnableDutyCycle      = "at+set_config=lora:dutycycle_enable:1"
	atLoraDisableDutyCycle     = "at+set_config=lora:dutycycle_enable:0"
	atLoraSetSendRepeatCount   = "at+set_config=lora:send_repeat_cnt:%d"
	atLoraRestoreDefaultParams = "at+set_config=lora:default_parameters"
	atLoraModeLoRaWAN          = "at+set_config=lora:work_mode:0"
	atLoraModeLoRaP2P          = "at+set_config=lora:work_mode:1"
	atLoraConfigP2P            = "at+set_config=lorap2p:%d:%d:%d:%d:%d:%d"
	atLoraReceiverP2P          = "at+set_config=lorap2p:transfer_mode:1"
	atLoraSenderP2P            = "at+set_config=lorap2p:transfer_mode:2"
	atLoraSendP2P              = "at+send=lorap2p:%02X"
)

// Command data
type Command struct {
	AT          string       `json:"at"`
	Timeout     opt.Duration `json:"timeout"`
	RegexOK     string       `json:"regexOk"`
	RegexError  string       `json:"regexError"`
	RegexWakeup string       `json:"regexWakeup"`
}

// NewCommand create command with timeout and default configuration
func NewCommand(at string, timeout time.Duration) *Command {
	return &Command{
		AT:      at,
		Timeout: opt.Duration{Duration: timeout},
	}
}

// ExpressionOK return regex for OK response
func (c *Command) ExpresionOK() (*regexp.Regexp, error) {
	expr := c.RegexOK
	if expr == "" {
		expr = atcmd.OkPattern
	}
	return regexp.Compile(expr)
}

// ExpressionError return regex for error response
func (c *Command) ExpresionError() (*regexp.Regexp, error) {
	expr := c.RegexError
	if expr == "" {
		expr = atcmd.ErrPattern
	}
	return regexp.Compile(expr)
}

// ExpressionWakeup return regex for wakeup response
func (c *Command) ExpresionWakeup() (*regexp.Regexp, error) {
	expr := c.RegexWakeup
	if expr == "" {
		expr = string(WakeUp)
	}
	return regexp.Compile(expr)
}
