package dashboard

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

const ambientInterval = time.Second / 2
const stormDuration = 2 * time.Second

type point struct{ x, y int }
type region struct{ x, y, width, height int }
type star struct {
	point
	seed uint64
}
type sparkle struct{ glyph, kind string }

func (r region) contains(p point) bool {
	return p.x >= r.x && p.x < r.x+r.width && p.y >= r.y && p.y < r.y+r.height
}

func (m *Model) backgroundRegions() []region {
	if m.width < 28 || m.height < 8 {
		return nil
	}
	header := m.header()
	var regions []region
	if m.expanded() {
		width := 0
		for _, row := range header[:4] {
			width = max(width, ansi.StringWidth(row))
		}
		x := m.padding() + width + 2
		if available := m.width - m.padding() - x; available > 0 {
			regions = append(regions, region{x, 0, available, 4})
		}
	}
	// Whole provider rows stay protected, including their interior whitespace.
	start := len(header) + len(m.body()) + 1
	if height := m.height - 2 - start; height > 0 {
		regions = append(regions, region{m.padding(), start, m.innerWidth(), height})
	}
	return regions
}

func starSeed(p point) uint64 {
	n := uint64(p.x)*0x9e3779b97f4a7c15 + uint64(p.y)*0xbf58476d1ce4e5b9
	n = (n ^ (n >> 30)) * 0xbf58476d1ce4e5b9
	n = (n ^ (n >> 27)) * 0x94d049bb133111eb
	return n ^ (n >> 31)
}

func backgroundStars(regions []region) []star {
	var stars []star
	for _, r := range regions {
		for y := r.y; y < r.y+r.height; y++ {
			for x := r.x; x < r.x+r.width; x++ {
				p := point{x, y}
				seed := starSeed(p)
				if seed%70 == 0 {
					stars = append(stars, star{p, seed})
				}
			}
		}
	}
	sort.Slice(stars, func(i, j int) bool { return stars[i].seed < stars[j].seed })
	return stars[:min(24, len(stars))]
}

func (m *Model) sceneElapsed() time.Duration {
	if !m.motion {
		return 0
	}
	return max(0, m.animationAt.Sub(m.sceneAt))
}

func travelRegion(regions []region) region {
	var best region
	for _, r := range regions {
		if r.width >= 20 && r.height >= 3 && r.width*r.height > best.width*best.height {
			best = r
		}
	}
	return best
}

func providerTier(s state) int {
	if s.snapshot == nil || s.err != nil {
		return 0
	}
	tier := 0
	for _, w := range s.snapshot.Windows {
		if w.UsedPercent >= 90 {
			return 2
		}
		if w.UsedPercent >= 70 {
			tier = 1
		}
	}
	return tier
}

func stormInterval(tier int) time.Duration {
	if tier == 2 {
		return 20 * time.Second
	}
	return 30 * time.Second
}

func (m *Model) updateStorm() {
	tier := 0
	for _, s := range m.states {
		tier = max(tier, providerTier(s))
	}
	if tier != m.stormTier {
		previous := m.stormTier
		m.stormTier, m.stormAt, m.nextStorm = tier, time.Time{}, time.Time{}
		if tier > 0 {
			m.nextStorm = m.animationAt.Add(stormInterval(tier))
			if m.motion && tier > previous {
				m.stormAt = m.animationAt
			}
		}
	}
	if !m.motion || tier == 0 {
		m.stormAt = time.Time{}
		return
	}
	if m.nextStorm.IsZero() || !m.animationAt.Before(m.nextStorm) {
		m.stormAt = m.animationAt
		m.nextStorm = m.animationAt.Add(stormInterval(tier))
	}
}

func (m *Model) stormActive() bool {
	elapsed := m.animationAt.Sub(m.stormAt)
	return m.motion && m.stormTier > 0 && !m.stormAt.IsZero() && elapsed >= 0 && elapsed < stormDuration
}

func (m *Model) borderKind(s state) string {
	if m.stormActive() && math.Cos(m.animationAt.Sub(m.stormAt).Seconds()*2*math.Pi) >= 0 {
		switch providerTier(s) {
		case 1:
			return "warn"
		case 2:
			return "bad"
		}
	}
	return "border"
}

func paintMeteor(points map[point]sparkle, r region, progress float64, seed uint64, kind string, reverse bool) {
	if progress < 0 || progress >= 1 || r.width == 0 {
		return
	}
	x := r.x - 4 + int(progress*float64(r.width+8))
	y := r.y + int(seed%uint64(r.height-2)) + int(progress*2)
	for i, glyph := range []string{"✦", "─", "·", "."} {
		p := point{x - i, y}
		if i > 1 {
			p.y--
		}
		if reverse {
			p.x = r.x + r.width - 1 - (p.x - r.x)
		}
		if r.contains(p) {
			points[p] = sparkle{glyph, kind}
		}
	}
}

func (m *Model) scenePoints(regions []region) map[point]sparkle {
	points := make(map[point]sparkle)
	elapsed := m.sceneElapsed()
	for _, s := range backgroundStars(regions) {
		period := time.Duration(6+s.seed%5) * time.Second
		phase := (int((elapsed%period)*8/period) + int(s.seed%8)) % 8
		glyph := []string{".", "·", "+", "✧", "✦", "✧", "+", "·"}[phase]
		kind := "star"
		if phase == 0 || phase == 7 {
			kind = "starDim"
		} else if phase == 3 || phase == 5 {
			kind = "starBright"
		} else if phase == 4 {
			kind = "accent"
		}
		points[s.point] = sparkle{glyph, kind}
	}
	r := travelRegion(regions)
	if m.visitActive(regions) {
		if m.visit.kind == "comet" {
			paintMeteor(points, m.visit.region, float64(m.animationAt.Sub(m.visit.at))/float64(cometDuration), m.visit.seed, "starBright", m.visit.reverse)
		} else {
			m.paintShip(points)
		}
	}
	if !m.expanded() || !m.stormActive() || r.width == 0 {
		return points
	}
	progress := float64(m.animationAt.Sub(m.stormAt)) / float64(stormDuration)
	if m.stormTier == 2 {
		for i := range 3 {
			paintMeteor(points, r, (progress-float64(i)*0.15)/0.65, uint64(i+1), "bad", false)
		}
		return points
	}
	for y := r.y; y < r.y+r.height; y++ {
		for x := r.x; x < r.x+r.width; x++ {
			dx := (float64(x-r.x) + 0.5 - float64(r.width)/2) / (float64(r.width) / 2)
			dy := (float64(y-r.y) + 0.5 - float64(r.height)/2) / (float64(r.height) / 2)
			p := point{x, y}
			if math.Abs(math.Hypot(dx, dy)-progress) < 0.1 && starSeed(p)%3 == 0 {
				points[p] = sparkle{"✧", "warn"}
			}
		}
	}
	return points
}

func (m *Model) background(rows []string) []string {
	points := m.scenePoints(m.backgroundRegions())
	positions := make([]point, 0, len(points))
	for p := range points {
		positions = append(positions, p)
	}
	sort.Slice(positions, func(i, j int) bool {
		if positions[i].y != positions[j].y {
			return positions[i].y < positions[j].y
		}
		return positions[i].x < positions[j].x
	})
	for _, p := range positions {
		width := ansi.StringWidth(rows[p.y])
		rows[p.y] += strings.Repeat(" ", p.x-width) + m.style(points[p].glyph, points[p].kind)
	}
	return rows
}
