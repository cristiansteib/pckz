package audio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Raw shapes of `pactl -f json` output. Only the fields we use are declared.

type rawVolume map[string]struct {
	Value int `json:"value"`
}

type rawPort struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Type         string `json:"type"`
	Priority     int    `json:"priority"`
	Availability string `json:"availability"`
}

type rawNode struct { // sink or source
	Index       int               `json:"index"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Mute        bool              `json:"mute"`
	Volume      rawVolume         `json:"volume"`
	Ports       []rawPort         `json:"ports"`
	ActivePort  string            `json:"active_port"`
	Properties  map[string]string `json:"properties"`
}

type rawCardPort struct {
	Description  string            `json:"description"`
	Type         string            `json:"type"`
	Priority     int               `json:"priority"`
	Availability string            `json:"availability"`
	Properties   map[string]string `json:"properties"`
	Profiles     []string          `json:"profiles"`
}

type rawProfile struct {
	Description string `json:"description"`
	Sinks       int    `json:"sinks"`
	Sources     int    `json:"sources"`
	Priority    int    `json:"priority"`
	Available   bool   `json:"available"`
}

type rawCard struct {
	Index         int                    `json:"index"`
	Name          string                 `json:"name"`
	ActiveProfile string                 `json:"active_profile"`
	Properties    map[string]string      `json:"properties"`
	Profiles      map[string]rawProfile  `json:"profiles"`
	Ports         map[string]rawCardPort `json:"ports"`
}

type rawStream struct { // sink-input
	Index      int               `json:"index"`
	Sink       int               `json:"sink"`
	Mute       bool              `json:"mute"`
	Corked     bool              `json:"corked"`
	Volume     rawVolume         `json:"volume"`
	Properties map[string]string `json:"properties"`
}

type rawList struct {
	Sinks      []rawNode   `json:"sinks"`
	Sources    []rawNode   `json:"sources"`
	SinkInputs []rawStream `json:"sink_inputs"`
	Cards      []rawCard   `json:"cards"`
}

type rawInfo struct {
	ServerName        string `json:"server_name"`
	DefaultSinkName   string `json:"default_sink_name"`
	DefaultSourceName string `json:"default_source_name"`
}

// pactl runs a pactl command and returns stdout.
func pactl(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pactl", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("pactl %s: %s", strings.Join(args, " "), msg)
	}
	return out, nil
}

func pactlJSON(v any, args ...string) error {
	out, err := pactl(append([]string{"-f", "json"}, args...)...)
	if err != nil {
		return err
	}
	return json.Unmarshal(out, v)
}

// percent averages the channel volumes into 0..150 percent.
func (v rawVolume) percent() int {
	if len(v) == 0 {
		return 0
	}
	sum := 0
	for _, ch := range v {
		sum += ch.Value
	}
	return int(float64(sum)/float64(len(v))/65536*100 + 0.5)
}

func itoa(i int) string { return strconv.Itoa(i) }
