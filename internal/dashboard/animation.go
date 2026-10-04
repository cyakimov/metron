package dashboard

import (
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/cyakimov/metron/internal/provider"
)

const frameInterval = time.Second / 8
const barDuration = 350 * time.Millisecond

type animationMsg time.Time

func (m *Model) advanceAnimation(at time.Time) {
	// Timer callbacks and provider results can arrive out of timestamp order.
	if at.After(m.animationAt) {
		m.animationAt = at
	}
}

func (m *Model) animate() tea.Cmd {
	if m.animationPending {
		return nil
	}
	delay := m.animationDelay()
	if delay == 0 {
		return nil
	}
	m.animationPending = true
	return tea.Tick(delay, func(t time.Time) tea.Msg { return animationMsg(t) })
}

func (m *Model) animationDelay() time.Duration {
	if !m.motion || m.width < 28 || m.height < 8 || m.ctx.Err() != nil {
		return 0
	}
	active := m.expanded() && (m.animationAt.Before(m.blinkUntil) || m.animationAt.Before(m.successUntil))
	for _, s := range m.states {
		active = active || (s.fetching && s.err == nil) || (len(s.barFrom) > 0 && s.err == nil && m.animationAt.Sub(s.barAt) < barDuration)
	}
	regions := m.backgroundRegions()
	if active || (m.expanded() && m.stormActive()) || m.cometActive(regions) {
		return frameInterval
	}
	if len(backgroundStars(regions)) > 0 {
		return ambientInterval
	}
	return 0
}

func (m *Model) frame() int {
	if !m.motion {
		return 0
	}
	return int(m.animationAt.UnixMilli()/frameInterval.Milliseconds()) % 4
}

func (m *Model) spinner() string {
	if !m.motion {
		return "◌"
	}
	return []string{"◐", "◓", "◑", "◒"}[m.frame()]
}

func (m *Model) barValue(s state, w provider.Window) float64 {
	from, ok := s.barFrom[w.ID]
	if !ok || !m.motion || s.err != nil {
		return w.UsedPercent
	}
	t := min(1, max(0, float64(m.animationAt.Sub(s.barAt))/float64(barDuration)))
	return from + (w.UsedPercent-from)*(1-math.Pow(1-t, 3))
}

func (m *Model) mood() (string, string, string) {
	var highest float64
	fetching, missing, failed := false, false, false
	for _, s := range m.states {
		failed = failed || s.err != nil
		fetching = fetching || s.fetching
		missing = missing || s.snapshot == nil
		if s.snapshot != nil {
			for _, w := range s.snapshot.Windows {
				highest = max(highest, w.UsedPercent)
			}
		}
	}
	switch {
	case failed:
		return "offline", "Check provider status below", "warn"
	case highest >= 90:
		return "alert", "Quota running low", "bad"
	case highest >= 70:
		return "concerned", "Keeping an eye on your quota", "warn"
	case fetching:
		return "scanning", "Scanning account limits", "accent"
	case missing:
		return "idle", "Waiting for account data", "muted"
	case m.motion && m.animationAt.Before(m.successUntil):
		return "success", "Account limits updated", "good"
	default:
		return "idle", "All systems live", "good"
	}
}

func (m *Model) mascot() []string {
	mood, _, kind := m.mood()
	eyes, mouth, jets := "o o", "~", "="
	switch mood {
	case "offline":
		eyes, mouth = "x x", "-"
	case "alert":
		eyes, mouth = "! !", "o"
	case "concerned":
		eyes, mouth = "o O", "-"
	case "scanning":
		eyes = []string{"o o", "o O", "O O", "O o"}[m.frame()]
	case "success":
		eyes, mouth = []string{"^ ^", "- -", "^ ^", "o o"}[m.frame()], "v"
	}
	if m.motion {
		if mood == "idle" && m.animationAt.Before(m.blinkUntil) {
			eyes = "- -"
		}
		if mood == "scanning" || m.animationAt.Before(m.blinkUntil) {
			jets = []string{"=", "~", "-", "~"}[m.frame()]
		} else if m.animationAt.Unix()%4 < 2 {
			jets = "~"
		}
	}
	rows := []string{"   .---.   ", "  /|" + eyes + "|\\  ", " (_|_" + mouth + "_|_) ", "  /=" + jets + "=" + jets + "=\\  "}
	if mood == "success" && m.frame()%2 == 1 {
		rows[0], rows[1], rows[2] = "           ", "  /.---.\\  ", " (_|^ ^|_) "
	}
	for i, row := range rows {
		rows[i] = m.style(row+strings.Repeat(" ", max(0, 11-len(row))), kind)
	}
	return rows
}
