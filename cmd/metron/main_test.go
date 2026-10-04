package main

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"
	"time"
)

func TestRefreshIntervalFlags(t *testing.T) {
	for _, tc := range []struct {
		name          string
		args          []string
		claude, codex time.Duration
	}{
		{"defaults", nil, 5 * time.Minute, time.Minute},
		{"global", []string{"--refresh-interval=2m"}, 2 * time.Minute, 2 * time.Minute},
		{"claude", []string{"--claude-refresh-interval=10m"}, 10 * time.Minute, time.Minute},
		{"codex", []string{"--codex-refresh-interval=30s"}, 5 * time.Minute, 30 * time.Second},
		{"both platforms", []string{"--claude-refresh-interval=10m", "--codex-refresh-interval=30s"}, 10 * time.Minute, 30 * time.Second},
		{"global then claude", []string{"--refresh-interval=2m", "--claude-refresh-interval=10m"}, 10 * time.Minute, 2 * time.Minute},
		{"claude then global", []string{"--claude-refresh-interval=10m", "--refresh-interval=2m"}, 10 * time.Minute, 2 * time.Minute},
		{"global then codex", []string{"--refresh-interval=2m", "--codex-refresh-interval=30s"}, 2 * time.Minute, 30 * time.Second},
		{"codex then global", []string{"--codex-refresh-interval=30s", "--refresh-interval=2m"}, 2 * time.Minute, 30 * time.Second},
		{"global and both platforms", []string{"--codex-refresh-interval=30s", "--refresh-interval=2m", "--claude-refresh-interval=10m"}, 10 * time.Minute, 30 * time.Second},
		{"explicit platform defaults", []string{"--claude-refresh-interval=5m", "--refresh-interval=2m", "--codex-refresh-interval=1m"}, 5 * time.Minute, time.Minute},
		{"fractional", []string{"--refresh-interval=1ms"}, time.Millisecond, time.Millisecond},
		{"hours", []string{"--refresh-interval=1h"}, time.Hour, time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			opts, err := parseOptions(tc.args, &output)
			if err != nil {
				t.Fatalf("parseOptions: %v; output: %s", err, &output)
			}
			if opts.refreshIntervals["claude"] != tc.claude || opts.refreshIntervals["codex"] != tc.codex {
				t.Fatalf("intervals = %v, want Claude %s and Codex %s", opts.refreshIntervals, tc.claude, tc.codex)
			}
			if output.Len() != 0 {
				t.Fatalf("unexpected output: %s", &output)
			}
		})
	}
}

func TestInvalidRefreshIntervals(t *testing.T) {
	for _, name := range []string{"refresh-interval", "claude-refresh-interval", "codex-refresh-interval"} {
		for _, value := range []string{"0", "-1s", "invalid", "", "999999999999999999999h"} {
			t.Run(name+"="+value, func(t *testing.T) {
				var output bytes.Buffer
				_, err := parseOptions([]string{"--" + name + "=" + value}, &output)
				if err == nil || !strings.Contains(output.String(), name) {
					t.Fatalf("error = %v; output: %s", err, &output)
				}
			})
		}
	}
	for _, args := range [][]string{
		{"--refresh-interval=0", "--claude-refresh-interval=5m", "--codex-refresh-interval=1m"},
		{"--claude-refresh-interval=0", "--refresh-interval=2m"},
		{"--refresh-interval=-1s", "--version"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var output bytes.Buffer
			_, err := parseOptions(args, &output)
			if err == nil || !strings.Contains(output.String(), "positive duration") {
				t.Fatalf("error = %v; output: %s", err, &output)
			}
		})
	}
}

func TestHelpDocumentsRefreshIntervals(t *testing.T) {
	var output bytes.Buffer
	_, err := parseOptions([]string{"--help"}, &output)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("error = %v, want help", err)
	}
	for _, text := range []string{"refresh-interval", "claude-refresh-interval", "codex-refresh-interval", "Claude every 5 minutes", "Codex every minute", "platform flags take precedence", "positive durations", "r refresh", "no-motion", "a toggle motion"} {
		if !strings.Contains(output.String(), text) {
			t.Errorf("help missing %q: %s", text, &output)
		}
	}
}

func TestMotionFlag(t *testing.T) {
	for _, tc := range []struct {
		args     []string
		noMotion bool
	}{
		{nil, false},
		{[]string{"--no-motion"}, true},
		{[]string{"--no-motion=false"}, false},
	} {
		var output bytes.Buffer
		opts, err := parseOptions(tc.args, &output)
		if err != nil || opts.noMotion != tc.noMotion {
			t.Fatalf("args %v: noMotion = %t, err = %v", tc.args, opts.noMotion, err)
		}
	}
}

func TestVersionAndUnexpectedArguments(t *testing.T) {
	var output bytes.Buffer
	opts, err := parseOptions([]string{"--version", "--refresh-interval=2m"}, &output)
	if err != nil || !opts.showVersion {
		t.Fatalf("version option = %t; error = %v", opts.showVersion, err)
	}
	_, err = parseOptions([]string{"unexpected"}, &output)
	if err == nil || !strings.Contains(output.String(), "unexpected arguments") {
		t.Fatalf("error = %v; output: %s", err, &output)
	}
}
