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
	m := New([]provider.Provider{stubProvider("claude"), stubProvider("codex")})
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
	for _, size := range [][2]int{{28, 8}, {40, 12}, {60, 20}, {80, 24}, {120, 30}} {
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
