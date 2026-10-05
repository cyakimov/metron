package dashboard

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/cyakimov/metron/internal/provider"
)

func TestMascotReactsToAccountState(t *testing.T) {
	for _, tc := range []struct {
		name, mood string
		used       float64
		fetching   bool
		failed     bool
		success    bool
	}{
		{"healthy", "idle", 38, false, false, false},
		{"warning boundary", "concerned", 70, false, false, false},
		{"critical boundary", "alert", 90, false, false, false},
		{"refreshing", "scanning", 38, true, false, false},
		{"warning precedes refresh", "concerned", 70, true, false, false},
		{"critical precedes refresh", "alert", 90, true, false, false},
		{"error precedes critical", "offline", 90, true, true, true},
		{"success", "success", 38, false, false, true},
		{"warning precedes success", "concerned", 70, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testModel(t)
			m.motion, m.animationAt = true, m.now
			m.states[0].snapshot.Windows[0].UsedPercent = tc.used
			m.states[0].fetching = tc.fetching
			if tc.failed {
				m.states[0].err = errors.New("connection lost")
			}
			if tc.success {
				m.successUntil = m.animationAt.Add(time.Second)
			}
			mood, _, _ := m.mood()
			if mood != tc.mood {
				t.Fatalf("mood = %s, want %s", mood, tc.mood)
			}
			if tc.failed && !strings.Contains(ansi.Strip(m.View().Content), "stale") {
				t.Fatal("mascot hid stale data status")
			}
		})
	}
}

func TestAnimationsPreserveLayoutAndControls(t *testing.T) {
	for _, size := range [][2]int{{28, 8}, {28, 12}, {40, 12}, {60, 15}, {60, 16}, {80, 24}, {120, 30}} {
		for _, dark := range []bool{false, true} {
			for _, color := range []bool{false, true} {
				m := testModel(t)
				m.width, m.height, m.dark, m.color, m.motion = size[0], size[1], dark, color, true
				m.states[0].fetching = true
				for _, elapsed := range []time.Duration{0, frameInterval, 2 * frameInterval, 3 * frameInterval} {
					m.animationAt = m.now.Add(elapsed)
					view := m.View().Content
					rows := strings.Split(view, "\n")
					if len(rows) != size[1] {
						t.Fatalf("%v: animation changed height to %d", size, len(rows))
					}
					for _, row := range rows {
						if ansi.StringWidth(row) > size[0] || strings.Contains(row, "…") {
							t.Fatalf("%v: clipped animation row %q", size, row)
						}
					}
					if !strings.Contains(ansi.Strip(rows[len(rows)-1]), "q quit") || !strings.Contains(rows[len(rows)-1], "a") {
						t.Fatalf("%v: controls missing: %q", size, rows[len(rows)-1])
					}
					for _, row := range m.mascot() {
						if ansi.StringWidth(row) != 11 {
							t.Fatalf("mascot frame changed width: %q", row)
						}
					}
					if !color && hasColor(view) {
						t.Fatal("monochrome view contains escape sequences")
					}
				}
			}
		}
	}
}

func TestAnimationClockNeverPollsProviders(t *testing.T) {
	m := testModel(t)
	m.motion, m.animationAt = true, m.now
	m.sceneAt = m.now
	m.states[0].fetching = true
	m.states[1].nextPoll = m.now.Add(-time.Second)
	before := m.states[1].nextPoll
	providerClock := m.now
	if m.animate() == nil || m.animate() != nil {
		t.Fatal("animation did not enforce a single pending timer")
	}
	m.Update(animationMsg(m.now.Add(10 * time.Second)))
	if m.states[1].fetching || m.states[1].nextPoll != before || m.now != providerClock {
		t.Fatal("animation frame affected provider polling")
	}
	if m.now.Equal(m.animationAt) {
		t.Fatal("animation frame advanced the provider clock")
	}
	m.states[0].fetching = false
	_, cmd := m.Update(animationMsg(m.animationAt.Add(frameInterval)))
	if cmd == nil || !m.animationPending || m.animationDelay() != ambientInterval {
		t.Fatal("animation did not return to the ambient cadence while idle")
	}
}

func TestMotionToggleStopsAndRestartsWithoutDuplicateTimers(t *testing.T) {
	m := testModel(t)
	m.motion, m.animationAt = true, time.Now()
	m.states[0].fetching = true
	m.animate()
	m.Update(tea.KeyPressMsg{Code: 'a'})
	frozen := m.View().Content
	_, cmd := m.Update(animationMsg(m.animationAt.Add(frameInterval)))
	if m.motion || cmd != nil || m.animationPending || m.View().Content != frozen {
		t.Fatal("disabled motion continued animating")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'a'})
	if !m.motion || cmd == nil || m.animate() != nil {
		t.Fatal("enabling motion did not start exactly one timer")
	}
	m.Update(tea.WindowSizeMsg{Width: 20, Height: 7})
	_, cmd = m.Update(animationMsg(m.animationAt.Add(frameInterval)))
	if cmd != nil || m.animationPending {
		t.Fatal("animation continued in an unusable pane")
	}
	_, cmd = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if cmd == nil {
		t.Fatal("animation did not resume after resize")
	}
	m.Close()
	_, cmd = m.Update(animationMsg(m.animationAt.Add(frameInterval)))
	if cmd != nil {
		t.Fatal("animation continued after shutdown")
	}
}

func TestHealthTransitionKeepsAuthoritativeNumbersAndFinishes(t *testing.T) {
	m := testModel(t)
	m.motion, m.animationAt = true, m.now
	m.sceneAt = m.now
	at := m.now
	fresh := provider.Snapshot{ObservedAt: at, Windows: []provider.Window{{ID: "5h", Label: "5h", UsedPercent: 82}}}
	m.Update(resultMsg{index: 0, at: at, snapshot: fresh})
	w := m.states[0].snapshot.Windows[0]
	if m.usageValue(m.states[0], w) != 38 || !strings.Contains(ansi.Strip(m.View().Content), "18% left") {
		t.Fatal("transition lost old health position or displayed an invented percentage")
	}
	m.Update(animationMsg(at.Add(frameInterval)))
	if value := m.usageValue(m.states[0], w); value <= 38 || value >= 82 {
		t.Fatalf("intermediate usage = %f", value)
	}
	m.Update(animationMsg(at.Add(healthDuration)))
	if m.usageValue(m.states[0], w) != 82 {
		t.Fatal("health did not reach the reported percentage")
	}
	m.Update(animationMsg(at.Add(3 * time.Second)))
	if m.animationDelay() != ambientInterval {
		t.Fatal("completed transition did not return to ambient animation")
	}
	m.states[0].healthAt = m.animationAt
	m.states[0].err = errors.New("connection lost")
	if m.usageValue(m.states[0], w) != 82 {
		t.Fatal("stale values retained an unfinished transition")
	}
}

func TestIdleBlinkAndSuccessExpireWithoutChangingQuota(t *testing.T) {
	m := testModel(t)
	m.motion, m.animationAt = true, m.now
	m.sceneAt = m.now
	m.nextBlink = m.now.Add(6 * time.Second)
	for i := range m.states {
		m.states[i].nextPoll = m.now.Add(time.Hour)
	}
	m.Update(tickMsg(m.now.Add(6 * time.Second)))
	if !strings.Contains(ansi.Strip(m.mascot()[1]), "- -") || !m.animationPending {
		t.Fatal("idle blink did not start")
	}
	_, cmd := m.Update(animationMsg(m.now.Add(250 * time.Millisecond)))
	if strings.Contains(ansi.Strip(m.mascot()[1]), "- -") || cmd == nil || m.animationDelay() != ambientInterval {
		t.Fatal("idle blink did not finish")
	}
	m.Update(tickMsg(m.now.Add(3 * time.Hour)))
	if m.states[0].snapshot.Windows[0].UsedPercent != 38 || !strings.Contains(ansi.Strip(m.View().Content), "reset pending") {
		t.Fatal("animation invented fresh quota after countdown expiry")
	}
	for i := range m.states {
		m.states[i].fetching = false
	}
	m.visit = ambientVisit{}
	m.nextVisit = m.animationAt.Add(time.Hour)
	m.successUntil = m.animationAt.Add(750 * time.Millisecond)
	if mood, _, _ := m.mood(); mood != "success" {
		t.Fatal("successful update did not produce a reaction")
	}
	m.Update(animationMsg(m.animationAt.Add(time.Second)))
	if mood, _, _ := m.mood(); mood != "idle" || m.animationDelay() != ambientInterval {
		t.Fatal("success reaction did not return to idle")
	}
}

func TestSmallPaneLoadingKeepsControlsAndUnavailableRemainsVisible(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 28, 12
	for i := range m.states {
		m.states[i].snapshot = nil
		m.states[i].fetching = true
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "q quit") || !strings.Contains(view, "a motion") {
		t.Fatal("compact loading view clipped controls")
	}
	m.Update(resultMsg{index: 0, at: m.now, err: errors.New("Sign in to Claude")})
	view = ansi.Strip(m.View().Content)
	if !strings.Contains(view, "unavailable") || !strings.Contains(view, "Sign in to Claude") || !strings.Contains(view, "CODEX") {
		t.Fatal("unavailable provider displaced healthy provider")
	}
}

func TestNoColorSurvivesTerminalThemeDetection(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := New([]provider.Provider{stubProvider("claude")}, map[string]time.Duration{"claude": time.Minute}, true)
	defer m.Close()
	m.Update(tea.BackgroundColorMsg{})
	if view := m.View().Content; hasColor(view) {
		t.Fatal("background detection overrode NO_COLOR")
	}
	m.color = true
	m.Update(tea.ColorProfileMsg{Profile: colorprofile.Ascii})
	if view := m.View().Content; hasColor(view) {
		t.Fatal("ASCII color profile retained color escapes")
	}
}

func TestQuotaColumnsAlignAcrossProviders(t *testing.T) {
	m := testModel(t)
	m.states[0].snapshot.Windows[0].Label = "Fable 5h"
	column := -1
	for _, row := range m.body() {
		if at := strings.Index(ansi.Strip(row), "% left"); at >= 0 {
			if column >= 0 && column != at {
				t.Fatalf("percentages shifted from column %d to %d: %q", column, at, row)
			}
			column = at
		}
	}
	if column < 0 {
		t.Fatal("quota rows missing")
	}
}

func TestSuccessNodPreservesMascotGeometry(t *testing.T) {
	m := testModel(t)
	m.motion, m.animationAt = true, m.now
	m.successUntil = m.now.Add(time.Second)
	before := strings.Join(m.mascot(), "\n")
	m.animationAt = m.animationAt.Add(frameInterval)
	after := strings.Join(m.mascot(), "\n")
	if before == after {
		t.Fatal("successful refresh did not animate a nod")
	}
	for _, frame := range []string{before, after} {
		rows := strings.Split(frame, "\n")
		if len(rows) != 4 {
			t.Fatal("nod changed mascot height")
		}
		for _, row := range rows {
			if ansi.StringWidth(row) != 11 {
				t.Fatal("nod changed mascot width")
			}
		}
	}
}

func hasColor(view string) bool {
	plain := strings.NewReplacer("\x1b[1m", "", "\x1b[0m", "", "\x1b[m", "").Replace(view)
	return ansi.Strip(plain) != plain
}
