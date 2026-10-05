package dashboard

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/cyakimov/metron/internal/provider"
)

const heartCount = 8
const damageDuration = 500 * time.Millisecond
const healingDuration = 750 * time.Millisecond

type reaction struct {
	at      time.Time
	healing bool
}

func (r reaction) duration() time.Duration {
	if r.healing {
		return healingDuration
	}
	return damageDuration
}

func (m *Model) reactionActive(s state, r reaction) bool {
	elapsed := m.animationAt.Sub(r.at)
	return m.motion && s.err == nil && !r.at.IsZero() && elapsed >= 0 && elapsed < r.duration()
}

func (m *Model) acceptSnapshot(s *state, snapshot provider.Snapshot) {
	from := make(map[string]float64)
	reactions := make(map[string]reaction)
	if m.motion && s.snapshot != nil {
		old := make(map[string]provider.Window, len(s.snapshot.Windows))
		for _, w := range s.snapshot.Windows {
			old[w.ID] = w
		}
		for _, fresh := range snapshot.Windows {
			if previous, ok := old[fresh.ID]; ok && previous.UsedPercent != fresh.UsedPercent {
				from[fresh.ID] = m.usageValue(*s, previous)
				reactions[fresh.ID] = reaction{m.animationAt, fresh.UsedPercent < previous.UsedPercent}
			}
		}
	}
	s.healthFrom, s.healthAt, s.reactions = from, m.animationAt, reactions
	s.snapshot = &snapshot
}

func healthUnits(used float64, units int) int {
	return int(math.Ceil(float64(units) * (100 - min(100, max(0, used))) / 100))
}

func remainingText(used float64) string {
	remaining := 100 - min(100, max(0, used))
	if remaining > 0 && remaining < 0.01 {
		return "<0.01% left"
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", remaining), "0"), ".") + "% left"
}

func padRight(text string, width int) string {
	return text + strings.Repeat(" ", max(0, width-ansi.StringWidth(text)))
}

func (m *Model) hearts(s state, w provider.Window) string {
	units := healthUnits(m.usageValue(s, w), heartCount)
	var hearts strings.Builder
	kind, separator, sparkleAt := "heart", " ", -1
	if r := s.reactions[w.ID]; m.reactionActive(s, r) {
		frame := int(m.animationAt.Sub(r.at) / frameInterval)
		if frame%2 == 0 {
			kind, separator = "heartFlash", "!"
			if r.healing {
				kind, separator = "good", "✧"
			}
			sparkleAt = frame % (heartCount - 1)
		}
	}
	for i := range heartCount {
		if i > 0 {
			gap := " "
			if i-1 == sparkleAt {
				gap = m.style(separator, kind)
			}
			hearts.WriteString(gap)
		}
		glyph, color := "♡", "track"
		if i < units {
			glyph, color = "♥", kind
		}
		hearts.WriteString(m.style(glyph, color))
	}
	return hearts.String()
}

type meterLayout struct {
	width, labelWidth, percentWidth, resetWidth int
}

func (m *Model) meterLayout() meterLayout {
	l := meterLayout{width: m.innerWidth(), labelWidth: 2}
	if m.expanded() {
		l.width -= 4
	}
	for _, s := range m.states {
		if s.snapshot == nil {
			continue
		}
		for _, w := range s.snapshot.Windows {
			l.labelWidth = max(l.labelWidth, ansi.StringWidth(w.Label))
			l.resetWidth = max(l.resetWidth, ansi.StringWidth(m.resetText(w)))
			l.percentWidth = max(l.percentWidth, ansi.StringWidth(remainingText(w.UsedPercent)))
		}
	}
	l.labelWidth = min(l.labelWidth, min(18, max(8, l.width/3)))
	return l
}

func (m *Model) window(s state, w provider.Window, l meterLayout) []string {
	kind := "good"
	if w.UsedPercent >= 90 {
		kind = "bad"
	} else if w.UsedPercent >= 70 {
		kind = "warn"
	}
	label := padRight(ansi.Truncate(w.Label, l.labelWidth, "…"), l.labelWidth)
	pct := remainingText(w.UsedPercent)
	pct = strings.Repeat(" ", max(0, l.percentWidth-ansi.StringWidth(pct))) + m.style(pct, kind)
	reset := m.style(m.resetText(w), "muted")
	hearts := m.hearts(s, w)
	line := "  " + label + "  "
	if ansi.StringWidth(line+hearts+"  "+pct) <= l.width {
		line += hearts + "  "
	}
	line += pct
	var rows []string
	if ansi.StringWidth(line)+2+l.resetWidth <= l.width {
		rows = []string{line + "  " + reset}
	} else {
		rows = strings.Split(ansi.Wrap(line, max(1, l.width), ""), "\n")
		rows = append(rows, m.wrapped(m.resetText(w), "muted", l.width)...)
	}
	if w.UsedPercent > 100 {
		rows = append(rows, m.wrapped(fmt.Sprintf("%.2f%% used - limit exceeded", w.UsedPercent), "bad", l.width)...)
	}
	return rows
}
