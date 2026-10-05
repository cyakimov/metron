package dashboard

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/cyakimov/metron/internal/provider"
)

func TestHealthStagesAndRemainingLabels(t *testing.T) {
	for _, tc := range []struct {
		used          float64
		full          int
		remainingText string
	}{
		{0, 8, "100% left"},
		{6.25, 8, "93.75% left"},
		{12.5, 7, "87.5% left"},
		{50, 4, "50% left"},
		{70, 3, "30% left"},
		{90, 1, "10% left"},
		{99.9, 1, "0.1% left"},
		{99.999, 1, "<0.01% left"},
		{100, 0, "0% left"},
		{125, 0, "0% left"},
	} {
		m := testModel(t)
		w := provider.Window{ID: "5h", Label: "5h", UsedPercent: tc.used}
		for _, theme := range [][2]bool{{true, true}, {false, true}, {true, false}} {
			m.dark, m.color = theme[0], theme[1]
			hearts := m.hearts(state{}, w)
			if strings.Count(hearts, "♥") != tc.full || strings.Count(hearts, "♡") != 8-tc.full {
				t.Fatalf("%.3f%% used: incorrect heart stages: %q", tc.used, hearts)
			}
			if ansi.StringWidth(hearts) != 15 || strings.Contains(hearts, "\n") || (!m.color && hasColor(hearts)) {
				t.Fatalf("heart meter changed geometry or monochrome rendering: %q", hearts)
			}
		}
		if got := remainingText(tc.used); got != tc.remainingText {
			t.Fatalf("%.3f%% used: %q, want %q", tc.used, got, tc.remainingText)
		}
	}
}

func TestHeartLayoutAdaptsToWidthHeightAndWindowCount(t *testing.T) {
	for _, size := range [][2]int{{28, 8}, {40, 12}, {60, 16}, {80, 16}, {80, 24}, {120, 30}} {
		m := testModel(t)
		m.width, m.height = size[0], size[1]
		body := strings.Join(m.body(), "\n")
		if !strings.Contains(body, "62% left") || strings.ContainsAny(body, "█▄▀") {
			t.Fatalf("%v: missing remaining quota or oversized hearts", size)
		}
		if size[0] >= 40 && strings.Count(body, "♥")+strings.Count(body, "♡") != 32 {
			t.Fatalf("%v: missing heart containers", size)
		}
		for _, line := range m.body() {
			if ansi.StringWidth(line) > m.innerWidth() {
				t.Fatalf("%v: meter overflowed: %q", size, line)
			}
		}
		if size[0] >= 60 && !strings.Contains(body, "╭─") {
			t.Fatalf("%v: provider frames missing", size)
		}
		if size[0] == 120 && len(m.body()) != 11 {
			t.Fatalf("wide pane did not keep each limit on one row: %q", body)
		}
	}
	m := testModel(t)
	for i := range 8 {
		m.states[0].snapshot.Windows = append(m.states[0].snapshot.Windows, provider.Window{ID: string(rune('a' + i)), Label: "Model-specific 7d", UsedPercent: 50})
	}
	if strings.Count(strings.Join(m.body(), "\n"), "♥")+strings.Count(strings.Join(m.body(), "\n"), "♡") != 96 {
		t.Fatal("many windows lost heart containers")
	}
	for _, line := range m.body() {
		if ansi.StringWidth(line) > m.innerWidth() {
			t.Fatalf("long label overflowed: %q", line)
		}
	}
}

func TestOverLimitKeepsReportedUsageAndWrapsInSmallPanes(t *testing.T) {
	for _, size := range [][2]int{{28, 8}, {60, 16}, {80, 24}, {120, 30}} {
		m := testModel(t)
		m.width, m.height = size[0], size[1]
		m.states[0].snapshot.Windows[0].UsedPercent = 125
		body := strings.Join(m.body(), "\n")
		if !strings.Contains(body, "0% left") || !strings.Contains(body, "125.00% used") {
			t.Fatal("over-limit usage was discarded")
		}
		for _, row := range m.body() {
			if ansi.StringWidth(row) > m.innerWidth() {
				t.Fatalf("over-limit usage overflowed: %q", row)
			}
		}
	}
}

func TestPerWindowDamageHealingAndExpiry(t *testing.T) {
	m := sceneModel(t)
	at := m.now
	fresh := provider.Snapshot{ObservedAt: at, Windows: []provider.Window{
		{ID: "5h", Label: "5h", UsedPercent: 55},
		{ID: "7d", Label: "7d", UsedPercent: 20},
		{ID: "new", Label: "New limit", UsedPercent: 10},
	}}
	m.Update(resultMsg{index: 0, at: at, snapshot: fresh})
	s := m.states[0]
	if len(s.reactions) != 2 || s.reactions["5h"].healing || !s.reactions["7d"].healing {
		t.Fatal("reactions did not match individual window changes")
	}
	for i, marker := range []string{"!", "✧"} {
		hearts := m.hearts(s, fresh.Windows[i])
		if !strings.Contains(hearts, marker) || ansi.StringWidth(hearts) != 15 {
			t.Fatalf("monochrome reaction missing %q or changed meter width: %q", marker, hearts)
		}
	}
	m.Update(animationMsg(at.Add(damageDuration)))
	if m.reactionActive(s, s.reactions["5h"]) || !m.reactionActive(s, s.reactions["7d"]) || m.animationDelay() != frameInterval {
		t.Fatal("damage/healing did not have independent lifetimes")
	}
	m.Update(animationMsg(at.Add(healingDuration)))
	if m.reactionActive(s, s.reactions["7d"]) || m.animationDelay() != ambientInterval {
		t.Fatal("completed reactions did not return to idle cadence")
	}
	m.Update(resultMsg{index: 0, at: at.Add(time.Second), snapshot: fresh})
	if len(m.states[0].reactions) != 0 {
		t.Fatal("unchanged snapshot replayed reactions")
	}
}

func TestReactionsRespectMissingFailedAndPausedData(t *testing.T) {
	m := sceneModel(t)
	m.states[0].snapshot = nil
	freshUsage(m, 0, m.now, 50)
	if len(m.states[0].reactions) != 0 {
		t.Fatal("first snapshot caused damage")
	}
	freshUsage(m, 0, m.now, 60)
	m.Update(resultMsg{index: 0, at: m.now, err: errors.New("offline")})
	if m.reactionActive(m.states[0], m.states[0].reactions["5h"]) || m.usageValue(m.states[0], m.states[0].snapshot.Windows[0]) != 60 {
		t.Fatal("failed snapshot continued health animation")
	}
	freshUsage(m, 0, m.now, 60)
	if len(m.states[0].reactions) != 0 {
		t.Fatal("recovery without quota change replayed damage")
	}
	m.Update(tea.KeyPressMsg{Code: 'a'})
	freshUsage(m, 0, m.now, 10)
	if len(m.states[0].reactions) != 0 || len(m.states[0].healthFrom) != 0 || !strings.Contains(ansi.Strip(m.View().Content), "90% left") {
		t.Fatal("paused update animated or hid fresh data")
	}
	m.Update(tea.KeyPressMsg{Code: 'a'})
	if len(m.states[0].reactions) != 0 {
		t.Fatal("resume replayed a missed healing effect")
	}
}
