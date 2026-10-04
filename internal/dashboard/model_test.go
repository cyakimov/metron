package dashboard

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/cyakimov/metron/internal/provider"
)

type stubProvider string

func (p stubProvider) ID() string { return string(p) }
func (p stubProvider) Fetch(context.Context) (provider.Snapshot, error) {
	return provider.Snapshot{}, nil
}

func testModel(t *testing.T) *Model {
	t.Helper()
	m := New([]provider.Provider{stubProvider("claude"), stubProvider("codex")}, map[string]time.Duration{"claude": 5 * time.Minute, "codex": time.Minute}, false)
	t.Cleanup(m.Close)
	m.color = false
	m.now = time.Date(2030, 1, 1, 10, 0, 0, 0, time.UTC)
	reset := m.now.Add(2*time.Hour + 14*time.Minute)
	for i := range m.states {
		m.states[i].snapshot = &provider.Snapshot{ObservedAt: m.now, Windows: []provider.Window{{ID: "5h", Label: "5h", UsedPercent: 38, ResetsAt: &reset}, {ID: "7d", Label: "7d", UsedPercent: 61}}, Details: []string{"Credits  15 left"}}
	}
	return m
}

func TestViewsFitSmallAndLargePanes(t *testing.T) {
	for _, size := range [][2]int{{28, 8}, {28, 12}, {40, 12}, {60, 16}, {60, 20}, {80, 24}, {120, 30}} {
		m := testModel(t)
		m.width, m.height = size[0], size[1]
		view := m.View()
		if !view.AltScreen {
			t.Fatal("alternate screen disabled")
		}
		lines := strings.Split(view.Content, "\n")
		if len(lines) != size[1] {
			t.Fatalf("%v: %d lines", size, len(lines))
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("%v: overflow %q", size, line)
			}
		}
		if !strings.Contains(ansi.Strip(view.Content), "q quit") {
			t.Fatal("quit control missing")
		}
	}
}

func TestStaleStateSurvivesFailureAndRefresh(t *testing.T) {
	m := testModel(t)
	before := m.states[0].snapshot
	m.Update(resultMsg{index: 0, at: m.now.Add(2 * time.Minute), err: &provider.Problem{Message: "Claude usage unavailable"}})
	if m.states[0].snapshot != before || m.states[1].err != nil {
		t.Fatal("failure changed good snapshots")
	}
	m.states[0].fetching = true
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "stale") || !strings.Contains(view, "38% used") || !strings.Contains(view, "live") {
		t.Fatal(view)
	}
	m.Update(resultMsg{index: 0, at: m.now, snapshot: provider.Snapshot{ObservedAt: m.now, Windows: []provider.Window{{ID: "5h", Label: "5h", UsedPercent: 42}}}})
	if m.states[0].err != nil || m.states[0].snapshot.Windows[0].UsedPercent != 42 {
		t.Fatal("refresh did not recover")
	}
}

func TestResetDoesNotInventFreshQuota(t *testing.T) {
	m := testModel(t)
	m.now = m.now.Add(3 * time.Hour)
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "reset pending") || !strings.Contains(view, "38% used") || !strings.Contains(view, "reset time unavailable") {
		t.Fatal(view)
	}
}

func TestManualRefreshHonorsBackoffAndInFlightRequests(t *testing.T) {
	m := testModel(t)
	m.now = time.Now()
	m.states[0].err = &provider.Problem{Message: "Slow down", RetryAt: m.now.Add(5 * time.Minute)}
	m.states[1].fetching = true
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'r'})
	if cmd != nil {
		t.Fatal("manual refresh bypassed backoff or in-flight request")
	}
	m.states[1].fetching = false
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'r'})
	if cmd == nil || m.states[0].fetching || !m.states[1].fetching {
		t.Fatal("providers not refreshed independently")
	}
}

func TestStartupFetchesBothProviders(t *testing.T) {
	m := testModel(t)
	if cmd := m.Init(); cmd == nil || !m.states[0].fetching || !m.states[1].fetching {
		t.Fatal("startup did not fetch both providers immediately")
	}
}

func TestAutomaticRefreshUsesProviderIntervals(t *testing.T) {
	for _, tc := range []struct {
		name          string
		claude, codex time.Duration
	}{
		{"defaults", 5 * time.Minute, time.Minute},
		{"overrides", 30 * time.Second, 2 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, elapsed := range []time.Duration{tc.claude - time.Nanosecond, tc.claude, tc.codex - time.Nanosecond, tc.codex} {
				m := testModel(t)
				m.states[0].interval, m.states[1].interval = tc.claude, tc.codex
				at := m.now.Add(15 * time.Second)
				for i := range m.states {
					m.Update(resultMsg{index: i, at: at, snapshot: *m.states[i].snapshot})
					if want := at.Add(m.states[i].interval); !m.states[i].nextPoll.Equal(want) {
						t.Fatalf("%s next poll = %s, want %s", m.states[i].provider.ID(), m.states[i].nextPoll, want)
					}
				}
				m.Update(tickMsg(at.Add(elapsed)))
				for _, s := range m.states {
					if want := elapsed >= s.interval; s.fetching != want {
						t.Errorf("at %s: %s fetching = %t, want %t", elapsed, s.provider.ID(), s.fetching, want)
					}
				}
			}
		})
	}
}

func TestAutomaticRefreshHonorsRetryDeadlines(t *testing.T) {
	for _, delay := range []time.Duration{2 * time.Minute, 10 * time.Minute} {
		m := testModel(t)
		at := m.now
		m.Update(resultMsg{index: 0, at: at, err: &provider.Problem{Message: "Slow down", RetryAt: at.Add(delay)}})
		want := at.Add(max(delay, m.states[0].interval))
		if !m.states[0].nextPoll.Equal(want) {
			t.Fatalf("next poll = %s, want %s", m.states[0].nextPoll, want)
		}
		m.Update(tickMsg(want.Add(-time.Nanosecond)))
		if m.states[0].fetching {
			t.Fatal("Claude fetched before its retry deadline")
		}
		m.Update(tickMsg(want))
		if !m.states[0].fetching {
			t.Fatal("Claude did not fetch at its retry deadline")
		}
	}
}

func TestAutomaticRefreshDoesNotOverlapInFlightRequests(t *testing.T) {
	m := testModel(t)
	first := m.poll(0)
	if first == nil {
		t.Fatal("initial poll missing")
	}
	m.Update(tickMsg(m.now.Add(time.Hour)))
	if cmd := m.poll(0); cmd != nil {
		t.Fatal("scheduled refresh allowed an overlapping request")
	}
	m.Update(first())
	if m.states[0].fetching {
		t.Fatal("in-flight state survived request completion")
	}
}

func TestManualRefreshCanFetchBeforeAutomaticDeadline(t *testing.T) {
	m := testModel(t)
	for i := range m.states {
		m.states[i].nextPoll = time.Now().Add(time.Hour)
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'r'})
	if cmd == nil || !m.states[0].fetching || !m.states[1].fetching {
		t.Fatal("manual refresh waited for the automatic deadline")
	}
}

func TestHeaderShowsEffectiveRefreshIntervals(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width         int
		claude, codex time.Duration
		label         string
	}{
		{"defaults at minimum width", 28, 5 * time.Minute, time.Minute, "Claude 5m, Codex 1m"},
		{"custom", 80, 10 * time.Minute, 30 * time.Second, "Claude 10m, Codex 30s"},
		{"equal", 80, 2 * time.Minute, 2 * time.Minute, "Claude 2m, Codex 2m"},
		{"hours and fractions", 120, time.Hour, time.Millisecond, "Claude 1h, Codex 1ms"},
		{"narrow custom", 28, 90 * time.Second, 30 * time.Second, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testModel(t)
			m.width = tc.width
			m.states[0].interval, m.states[1].interval = tc.claude, tc.codex
			header := ansi.Strip(strings.Join(m.header(), "\n"))
			if tc.label == "" {
				if strings.TrimSpace(header) != "metron" {
					t.Fatalf("narrow header = %q, want title only", header)
				}
			} else if !strings.Contains(header, tc.label) {
				t.Fatalf("header = %q, want %q", header, tc.label)
			}
			for _, line := range strings.Split(header, "\n") {
				if ansi.StringWidth(line) > m.innerWidth() || strings.Contains(line, "…") {
					t.Fatalf("header clipped or overflowed: %q", header)
				}
			}
		})
	}
}

func TestScrollResizeAndQuit(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 40, 8
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	if m.offset == 0 {
		t.Fatal("long dashboard did not scroll")
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	if m.offset != 0 {
		t.Fatal("resize did not clamp scroll")
	}
	m.Update(tea.KeyPressMsg{Code: 'q'})
	if !errors.Is(m.ctx.Err(), context.Canceled) {
		t.Fatal("quit did not cancel requests")
	}
}
