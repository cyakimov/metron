package dashboard

import (
	"strings"
	"time"
)

const cometDuration = 1500 * time.Millisecond
const shipDuration = 6 * time.Second
const visitQuietMin = 12 * time.Second
const visitQuietMax = 25 * time.Second

type ambientVisit struct {
	kind    string
	at      time.Time
	region  region
	seed    uint64
	reverse bool
	sprite  int
}

func (v ambientVisit) duration() time.Duration {
	if v.kind == "comet" {
		return cometDuration
	}
	return shipDuration
}

func (m *Model) visitActive(regions []region) bool {
	elapsed := m.animationAt.Sub(m.visit.at)
	if !m.motion || !m.expanded() || m.stormActive() || m.visit.at.IsZero() || elapsed < 0 || elapsed >= m.visit.duration() {
		return false
	}
	for _, r := range regions {
		if r == m.visit.region {
			return true
		}
	}
	return false
}

func (m *Model) updateAmbient() {
	regions := m.backgroundRegions()
	r := travelRegion(regions)
	if !m.motion || !m.expanded() || m.ctx.Err() != nil || m.stormActive() || r.width == 0 {
		m.visit, m.nextVisit = ambientVisit{}, time.Time{}
		return
	}
	if !m.visit.at.IsZero() {
		if m.visitActive(regions) {
			return
		}
		m.visit, m.nextVisit = ambientVisit{}, time.Time{}
	}
	if m.nextVisit.IsZero() {
		quiet := visitQuietMin + time.Duration(m.random.Int64N(int64(visitQuietMax-visitQuietMin)+1))
		m.nextVisit = m.animationAt.Add(quiet)
		return
	}
	if m.animationAt.Before(m.nextVisit) {
		return
	}
	kind := "ship"
	if m.random.IntN(10) >= 6 {
		kind = "comet"
	}
	m.visit = ambientVisit{kind: kind, at: m.animationAt, region: r, seed: m.random.Uint64(), reverse: m.random.IntN(2) == 1, sprite: m.random.IntN(2)}
	m.nextVisit = time.Time{}
}

func mirrorGlyph(glyph rune) rune {
	switch glyph {
	case '/':
		return '\\'
	case '\\':
		return '/'
	case '<':
		return '>'
	case '>':
		return '<'
	case '(':
		return ')'
	case ')':
		return '('
	default:
		return glyph
	}
}

func (m *Model) paintShip(points map[point]sparkle) {
	v := m.visit
	progress := float64(m.animationAt.Sub(v.at)) / float64(shipDuration)
	jet := []string{"~", "=", "-", "="}[m.frame()]
	rows := []string{"   /\\__ ", jet + "=<____>"}
	if v.sprite == 1 {
		rows = []string{" .-oo-. ", "(_" + strings.Repeat(jet, 4) + "_)"}
	}
	width := len(rows[0])
	x := v.region.x - width + int(progress*float64(v.region.width+width))
	if v.reverse {
		x = v.region.x + v.region.width - int(progress*float64(v.region.width+width))
	}
	y := v.region.y + int(v.seed%uint64(v.region.height-1))
	for dy, row := range rows {
		for dx, glyph := range row {
			if v.reverse {
				dx, glyph = width-1-dx, mirrorGlyph(glyph)
			}
			p := point{x + dx, y + dy}
			if !v.region.contains(p) {
				continue
			}
			if glyph == ' ' {
				delete(points, p)
			} else {
				points[p] = sparkle{string(glyph), "accent"}
			}
		}
	}
}
