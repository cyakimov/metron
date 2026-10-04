package dashboard

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/cyakimov/metron/internal/provider"
)

func sceneModel(t *testing.T) *Model {
	t.Helper()
	m := testModel(t)
	m.motion, m.animationAt, m.sceneAt = true, m.now, m.now
	return m
}

func freshUsage(m *Model, index int, at time.Time, used float64) {
	m.Update(resultMsg{index: index, at: at, snapshot: provider.Snapshot{ObservedAt: at, Windows: []provider.Window{{ID: "5h", Label: "5h", UsedPercent: used}}}})
}

func TestBackgroundPreservesForegroundAcrossScenesAndScrolling(t *testing.T) {
	for _, size := range [][2]int{{28, 8}, {28, 12}, {40, 12}, {60, 16}, {80, 24}, {120, 30}} {
		for _, theme := range [][2]bool{{true, true}, {false, true}, {true, false}} {
			for _, used := range []float64{38, 70, 90} {
				m := sceneModel(t)
				m.width, m.height, m.dark, m.color = size[0], size[1], theme[0], theme[1]
				freshUsage(m, 0, m.now, used)
				for _, elapsed := range []time.Duration{500 * time.Millisecond, 1250 * time.Millisecond, cometPeriod + 750*time.Millisecond} {
					m.animationAt = m.sceneAt.Add(elapsed)
					assertScenePreservesForeground(t, m)
					m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
					assertScenePreservesForeground(t, m)
					m.Update(tea.KeyPressMsg{Code: tea.KeyHome})
				}
			}
		}
	}
}

func assertScenePreservesForeground(t *testing.T, m *Model) {
	t.Helper()
	before := m.foregroundRows()
	decorated := m.background(append([]string(nil), before...))
	for y, row := range decorated {
		if !strings.HasPrefix(row, before[y]) || ansi.StringWidth(row) > m.width {
			t.Fatalf("%dx%d: background changed or overflowed foreground row %d", m.width, m.height, y)
		}
	}
	for p := range m.scenePoints(m.backgroundRegions()) {
		if p.y >= len(m.header()) && p.y < len(m.header())+min(len(m.body()), m.bodyHeight())+1 {
			t.Fatalf("decoration entered provider rows at %v", p)
		}
	}
	view := m.View().Content
	if len(strings.Split(view, "\n")) != m.height || (!m.color && hasColor(view)) || !strings.Contains(ansi.Strip(view), "q quit") {
		t.Fatal("scene changed dimensions, monochrome rendering, or controls")
	}
}

func TestStarsStayFixedWhileTwinklingAndOnResize(t *testing.T) {
	m := sceneModel(t)
	regions := m.backgroundRegions()
	stars := backgroundStars(regions)
	if len(stars) == 0 || len(stars) > 24 {
		t.Fatalf("unexpected star count: %d", len(stars))
	}
	initial := m.scenePoints(regions)
	m.animationAt = m.animationAt.Add(2 * time.Second)
	after := m.scenePoints(regions)
	if reflect.DeepEqual(initial, after) {
		t.Fatal("stars did not twinkle")
	}
	for p := range initial {
		if _, ok := after[p]; !ok {
			t.Fatal("twinkle moved a star")
		}
	}
	if !reflect.DeepEqual(stars, backgroundStars(regions)) {
		t.Fatal("star generation is nondeterministic")
	}
	m.width, m.height = 120, 30
	for _, s := range backgroundStars(m.backgroundRegions()) {
		if s.seed != starSeed(s.point) {
			t.Fatal("resize changed the coordinate's star seed")
		}
	}
	if count := len(backgroundStars([]region{{0, 0, 200, 80}})); count != 24 {
		t.Fatalf("large viewport exceeded star budget: %d", count)
	}
}

func TestStormEntryRecurrenceEscalationAndDeescalation(t *testing.T) {
	m := sceneModel(t)
	at := m.now
	freshUsage(m, 0, at, 70)
	if !m.stormActive() || m.stormTier != 1 || m.nextStorm != at.Add(30*time.Second) {
		t.Fatal("warning did not start on first snapshot")
	}
	started := m.stormAt
	freshUsage(m, 0, at.Add(time.Second), 82)
	if m.stormAt != started || m.nextStorm != at.Add(30*time.Second) {
		t.Fatal("ordinary refresh restarted a warning storm")
	}
	m.Update(animationMsg(at.Add(stormDuration)))
	if m.stormActive() || m.animationDelay() != ambientInterval {
		t.Fatal("storm did not end after two seconds")
	}
	m.Update(animationMsg(at.Add(30 * time.Second)))
	if !m.stormActive() || m.stormAt != at.Add(30*time.Second) {
		t.Fatal("warning reminder did not recur")
	}
	freshUsage(m, 1, at.Add(31*time.Second), 90)
	if !m.stormActive() || m.stormTier != 2 || m.nextStorm != at.Add(51*time.Second) {
		t.Fatal("critical quota did not escalate immediately")
	}
	if m.borderKind(m.states[0]) != "warn" || m.borderKind(m.states[1]) != "bad" {
		t.Fatal("border colors did not reflect individual provider severity")
	}
	m.Update(animationMsg(at.Add(51 * time.Second)))
	if !m.stormActive() || m.stormAt != at.Add(51*time.Second) {
		t.Fatal("critical reminder did not recur after twenty seconds")
	}
	freshUsage(m, 1, at.Add(52*time.Second), 20)
	if m.stormTier != 1 || m.stormActive() || m.nextStorm != at.Add(82*time.Second) {
		t.Fatal("lower fresh usage did not de-escalate the storm")
	}
	freshUsage(m, 0, at.Add(53*time.Second), 20)
	if m.stormTier != 0 || !m.nextStorm.IsZero() || m.stormActive() {
		t.Fatal("healthy usage did not stop warnings")
	}
}

func TestStaleSnapshotsDoNotDriveStormsOrResetQuota(t *testing.T) {
	m := sceneModel(t)
	at := m.now
	freshUsage(m, 0, at, 94)
	freshUsage(m, 1, at.Add(time.Second), 76)
	m.Update(resultMsg{index: 0, at: at.Add(2 * time.Second), err: errors.New("connection lost")})
	if m.stormTier != 1 || providerTier(m.states[0]) != 0 || m.borderKind(m.states[0]) != "border" {
		t.Fatal("stale critical values drove the background storm")
	}
	m.Update(resultMsg{index: 1, at: at.Add(3 * time.Second), err: errors.New("connection lost")})
	if m.stormTier != 0 || m.stormActive() || !strings.Contains(ansi.Strip(m.View().Content), "94% used") {
		t.Fatal("all-stale data kept warnings active or discarded reported usage")
	}
	freshUsage(m, 0, at.Add(4*time.Second), 94)
	reset := at.Add(5 * time.Second)
	m.states[0].snapshot.Windows[0].ResetsAt = &reset
	m.Update(animationMsg(at.Add(10 * time.Second)))
	if m.stormTier != 2 || m.states[0].snapshot.Windows[0].UsedPercent != 94 {
		t.Fatal("countdown expiry changed quota severity")
	}
}

func TestScenePausesAndResumesWithoutReplayingMissedStorms(t *testing.T) {
	m := sceneModel(t)
	freshUsage(m, 0, time.Now(), 94)
	m.Update(tea.KeyPressMsg{Code: 'a'})
	points := m.scenePoints(m.backgroundRegions())
	m.Update(animationMsg(m.animationAt.Add(time.Hour)))
	if m.stormActive() || m.animationPending || m.animationDelay() != 0 || !reflect.DeepEqual(points, m.scenePoints(m.backgroundRegions())) {
		t.Fatal("paused scene kept moving or scheduling frames")
	}
	freshUsage(m, 0, time.Now(), 76)
	if m.stormActive() || m.stormTier != 1 {
		t.Fatal("paused scene ignored current quota data")
	}
	m.Update(tea.KeyPressMsg{Code: 'a'})
	if !m.stormActive() || m.stormTier != 1 || m.sceneElapsed() != 0 || m.nextStorm != m.animationAt.Add(30*time.Second) {
		t.Fatal("resume did not restart from current data")
	}
}

func TestAmbientAndTravelAnimationScheduling(t *testing.T) {
	m := sceneModel(t)
	if m.animationDelay() != ambientInterval {
		t.Fatal("ordinary twinkles did not use two frames per second")
	}
	m.animationAt = m.sceneAt.Add(cometPeriod + cometDuration/2)
	if !m.cometActive(m.backgroundRegions()) || m.animationDelay() != frameInterval {
		t.Fatal("comet did not use eight frames per second")
	}
	m.animationAt = m.sceneAt.Add(cometPeriod + cometDuration)
	if m.cometActive(m.backgroundRegions()) || m.animationDelay() != ambientInterval {
		t.Fatal("comet did not end and return to ambient cadence")
	}
	if m.animate() == nil || m.animate() != nil {
		t.Fatal("scene scheduled overlapping animation timers")
	}
	m.width, m.height = 28, 8
	if len(m.backgroundRegions()) != 0 || m.animationDelay() != 0 {
		t.Fatal("packed compact pane scheduled invisible decorations")
	}
	m.Update(animationMsg(m.animationAt.Add(frameInterval)))
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if cmd == nil || m.animationDelay() != ambientInterval {
		t.Fatal("starfield did not resume after resizing")
	}
	m.Close()
	if m.animationDelay() != 0 {
		t.Fatal("scene timer continued after shutdown")
	}
}

func TestCometsAndStormsRenderMovingEffects(t *testing.T) {
	m := sceneModel(t)
	regions := m.backgroundRegions()
	m.animationAt = m.sceneAt.Add(cometPeriod + cometDuration/3)
	first := meteorHead(m.scenePoints(regions), "starBright")
	m.animationAt = m.sceneAt.Add(cometPeriod + 2*cometDuration/3)
	second := meteorHead(m.scenePoints(regions), "starBright")
	if first.x < 0 || second.x <= first.x {
		t.Fatal("comet did not cross open background space")
	}
	freshUsage(m, 0, m.animationAt.Add(time.Second), 70)
	m.animationAt = m.stormAt.Add(stormDuration / 2)
	amber := false
	for _, s := range m.scenePoints(m.backgroundRegions()) {
		amber = amber || s.kind == "warn"
	}
	if !amber {
		t.Fatal("warning did not render an amber ripple")
	}
	freshUsage(m, 0, m.animationAt.Add(time.Second), 90)
	m.animationAt = m.stormAt.Add(stormDuration / 2)
	if meteorHead(m.scenePoints(m.backgroundRegions()), "bad").x < 0 {
		t.Fatal("critical quota did not render meteors")
	}
	m.width = 40
	for _, s := range m.scenePoints(m.backgroundRegions()) {
		if s.kind == "bad" || s.kind == "warn" || s.glyph == "─" {
			t.Fatal("compact pane rendered travelling effects")
		}
	}
}

func meteorHead(points map[point]sparkle, kind string) point {
	for p, s := range points {
		if s.kind == kind && s.glyph == "✦" {
			return p
		}
	}
	return point{-1, -1}
}

func TestOutOfOrderMessagesDoNotReverseSceneTime(t *testing.T) {
	m := sceneModel(t)
	at := m.now
	m.Update(animationMsg(at.Add(time.Second)))
	m.Update(tickMsg(at.Add(500 * time.Millisecond)))
	if m.animationAt != at.Add(time.Second) {
		t.Fatal("late polling tick reversed scene time")
	}
	freshUsage(m, 0, at.Add(750*time.Millisecond), 70)
	if m.animationAt != at.Add(time.Second) || m.stormAt != m.animationAt {
		t.Fatal("late provider result reversed scene time or storm entry")
	}
	m.Update(animationMsg(at.Add(875 * time.Millisecond)))
	if m.animationAt != at.Add(time.Second) {
		t.Fatal("late animation frame reversed scene time")
	}
}
