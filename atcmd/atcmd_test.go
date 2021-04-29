package atcmd

import (
	"context"
	"fmt"
	"regexp"
	"testing"
	"time"
)

func longOperation() {
	fmt.Println("Long operation...")
	time.Sleep(1 * time.Second)
}

func testSelect(ctx context.Context, ts time.Duration) {
	ticker := time.NewTicker(500 * time.Millisecond)
	select {
	case t := <-ticker.C:
		fmt.Println("Ticker at: ", t.Format(time.RFC3339))
	case <-ctx.Done():
		fmt.Println("Context DONE!")
	}
	ticker.Stop()
}

func testContext(ctx context.Context, n int, delay time.Duration) {
	for n > 0 {
		select {
		case <-ctx.Done():
			fmt.Println("Context DONE")
		default:
			fmt.Println("Default operation!")
		}
		n--
		time.Sleep(delay)
	}
}

func TestContext(t *testing.T) {
	testContext(context.TODO(), 10, 1*time.Second)
}

func TestRegex(t *testing.T) {
	lines := []string{
		"OK [information]\r\n",
		`OK [information]\r\n`,
		"ERROR: [ErrCode]\r\n",
		"ERROR: 80\r\n",
	}

	for _, line := range lines {
		matched, err := regexp.MatchString(`OK(.+)\r\n`, line)
		fmt.Println(line, ":", matched) // true
		fmt.Println(err)                // nil (regexp is valid)
	}
}
