package audio

import (
	"bufio"
	"os/exec"
	"strings"
	"sync"
	"time"

	"pckz/internal/bt"
)

// Watch calls fn (from a background goroutine) whenever devices, defaults,
// volumes or streams change. Bursts of events are coalesced. The sound
// server pushes the events, so nothing is polled.
func Watch(fn func()) {
	if Demo {
		demoMu.Lock()
		demoNotify = fn
		demoMu.Unlock()
		return
	}
	var mu sync.Mutex
	var timer *time.Timer
	trigger := func() {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(80*time.Millisecond, fn)
	}

	go func() {
		for {
			subscribe(trigger)
			// The server went away (e.g. a PipeWire restart): reconnect.
			time.Sleep(2 * time.Second)
			trigger()
		}
	}()
	bt.Watch(trigger)
}

func subscribe(trigger func()) {
	cmd := exec.Command("pactl", "subscribe")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}
	defer cmd.Wait()
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		// "Event 'change' on sink #57". Client events are ignored: every
		// pactl call we make would otherwise trigger a refresh loop.
		line := sc.Text()
		_, rest, ok := strings.Cut(line, " on ")
		if !ok {
			continue
		}
		facility, _, _ := strings.Cut(rest, " #")
		switch facility {
		case "sink", "source", "sink-input", "card", "server":
			trigger()
		}
	}
}
