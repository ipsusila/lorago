package rak

import (
	"fmt"
	"testing"
)

func TestFormatCommand(t *testing.T) {
	data := []byte{0x01, 0xA1, 0x11, 0xFF}
	cmd := fmt.Sprintf(atLoraSend, 1, data)
	t.Log("Command:", cmd)
}
