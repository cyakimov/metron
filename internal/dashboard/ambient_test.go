package dashboard

import (
	"reflect"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestSeededVisitsRespectQuietIntervalsAndDoNotCatchUp(t *testing.T) {
	a, b := sceneModel(t), sceneModel(t)
	kinds, sprites, directions := map[string]bool{}, map[int]bool{}, map[bool]bool{}
	for range 40 {
		for _, m := range []*Model{a, b} {
			m.updateAmbient()
			quiet := m.nextVisit.Sub(m.animationAt)
			if quiet < visitQuietMin || quiet > visitQuietMax {
				t.Fatalf("quiet interval outside bounds: %s", quiet)
			}
			deadline := m.nextVisit
			m.animationAt = deadline.Add(-time.Nanosecond)
			m.updateAmbient()
			if !m.visit.at.IsZero() {
				t.Fatal("visit started before its deadline")
			}
			m.animationAt = deadline
			m.updateAmbient()
			if !m.visitActive(m.backgroundRegions()) || !m.nextVisit.IsZero() {
				t.Fatal("scheduled visit did not start")
			}
		}
		if a.visit != b.visit {
			t.Fatal("identical seeds and timestamps produced different visits")
		}
		kinds[a.visit.kind], sprites[a.visit.sprite], directions[a.visit.reverse] = true, true, true
		for _, m := range []*Model{a, b} {
			m.animationAt = m.visit.at.Add(m.visit.duration())
			m.updateAmbient()
			if !m.visit.at.IsZero() || !m.nextVisit.After(m.animationAt) {
				t.Fatal("visit ended without a fresh quiet interval")
			}
		}
	}
	if len(kinds) != 2 || len(sprites) != 2 || len(directions) != 2 {
		t.Fatal("seeded visits did not exercise both kinds, sprites, and directions")
	}
	a.animationAt = a.nextVisit.Add(time.Hour)
	a.updateAmbient()
	if a.visit.at != a.animationAt {
		t.Fatal("delayed update replayed an old visit")
	}
	before := a.visit
	a.updateAmbient()
	if a.visit != before {
		t.Fatal("update restarted or overlapped an active visit")
	}
}

func TestVisitsMoveBothDirectionsWithoutChangingForeground(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 30}} {
		for _, kind := range []string{"comet", "ship"} {
			for _, reverse := range []bool{false, true} {
				for sprite := range 2 {
					m := sceneModel(t)
					m.width, m.height = size[0], size[1]
					m.visit = ambientVisit{kind: kind, at: m.now, region: travelRegion(m.backgroundRegions()), seed: 2, reverse: reverse, sprite: sprite}
					positions := []int{}
					for _, fraction := range []int{1, 2} {
						m.animationAt = m.visit.at.Add(time.Duration(fraction) * m.visit.duration() / 3)
						points := m.scenePoints(m.backgroundRegions())
						if !reflect.DeepEqual(points, m.scenePoints(m.backgroundRegions())) {
							t.Fatal("rendering consumed randomness")
						}
						left, count := m.width, 0
						for p, s := range points {
							if (kind == "comet" && s.glyph == "✦" && s.kind == "starBright") || (kind == "ship" && s.kind == "accent" && len(s.glyph) == 1) {
								left, count = min(left, p.x), count+1
							}
						}
						if count == 0 {
							t.Fatal("active visit had no visible sprite")
						}
						positions = append(positions, left)
						assertScenePreservesForeground(t, m)
					}
					if (!reverse && positions[1] <= positions[0]) || (reverse && positions[1] >= positions[0]) {
						t.Fatal("visit did not move in the requested direction")
					}
				}
			}
		}
	}
}

func TestVisitsCancelForStormsResizePauseAndShutdown(t *testing.T) {
	for _, action := range []string{"storm", "resize", "pause", "shutdown"} {
		m := sceneModel(t)
		m.visit = ambientVisit{kind: "ship", at: m.now, region: travelRegion(m.backgroundRegions())}
		m.animationAt = m.now.Add(shipDuration / 2)
		switch action {
		case "storm":
			freshUsage(m, 0, m.animationAt, 90)
		case "resize":
			m.Update(tea.WindowSizeMsg{Width: 28, Height: 8})
		case "pause":
			m.Update(tea.KeyPressMsg{Code: 'a'})
		case "shutdown":
			m.Close()
			m.updateAmbient()
		}
		if !m.visit.at.IsZero() || !m.nextVisit.IsZero() {
			t.Fatalf("%s did not cancel ambient state", action)
		}
	}
	m := sceneModel(t)
	m.updateAmbient()
	m.Update(tea.WindowSizeMsg{Width: 28, Height: 8})
	m.animationAt = m.animationAt.Add(time.Hour)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if !m.visit.at.IsZero() || m.nextVisit.Sub(m.animationAt) < visitQuietMin {
		t.Fatal("restored space replayed missed events")
	}
}

func TestProviderLayoutChangesCancelInvalidTravelRegions(t *testing.T) {
	m := sceneModel(t)
	m.height = 40
	m.visit = ambientVisit{kind: "ship", at: m.now, region: travelRegion(m.backgroundRegions())}
	m.animationAt = m.now.Add(time.Second)
	freshUsage(m, 0, m.animationAt, 40)
	if !m.visit.at.IsZero() || !m.nextVisit.After(m.animationAt) {
		t.Fatal("changed provider geometry retained an invalid sprite region")
	}
}
