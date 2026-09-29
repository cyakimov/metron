package dashboard

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/cyakimov/metron/internal/provider"
)

func (m *Model) style(text, kind string) string {
	s := lipgloss.NewStyle()
	if kind == "title" || kind == "claude" || kind == "codex" {
		s = s.Bold(true)
	}
	if m.color {
		dark := map[string]string{"muted": "#8A8A8A", "claude": "#D99A72", "codex": "#78BCCF", "good": "#78BA9B", "warn": "#D5B063", "bad": "#E27676"}
		light := map[string]string{"muted": "#686868", "claude": "#A8512A", "codex": "#1B7698", "good": "#267A51", "warn": "#9C6500", "bad": "#B73B3B"}
		palette := dark
		if !m.dark {
			palette = light
		}
		if color, ok := palette[kind]; ok {
			s = s.Foreground(lipgloss.Color(color))
		}
	}
	return s.Render(text)
}

func (m *Model) padding() int {
	if m.width >= 60 {
		return 2
	}
	return 1
}
func (m *Model) innerWidth() int { return max(1, m.width-2*m.padding()) }

func (m *Model) View() tea.View {
	if m.width < 28 || m.height < 8 {
		lines := []string{"metron", "Resize to at least 28 x 8", "q quit"}
		for i := range lines {
			lines[i] = ansi.Truncate(lines[i], max(1, m.width), "")
		}
		v := tea.NewView(strings.Join(lines[:min(len(lines), max(1, m.height))], "\n"))
		v.AltScreen = true
		return v
	}
	width := m.innerWidth()
	left := m.style("metron", "title")
	if width >= 60 {
		left += "  " + m.style("account limits", "muted")
	}
	var cadences []string
	for _, s := range m.states {
		name := s.provider.ID()
		switch name {
		case "claude":
			name = "Claude"
		case "codex":
			name = "Codex"
		}
		cadences = append(cadences, name+" "+refreshDuration(s.interval))
	}
	right := m.style(strings.Join(cadences, ", "), "muted")
	header := left
	if ansi.StringWidth(left)+1+ansi.StringWidth(right) <= width {
		header += strings.Repeat(" ", width-ansi.StringWidth(left)-ansi.StringWidth(right)) + right
	}
	body := m.body()
	start := min(m.offset, max(0, len(body)-m.bodyHeight()))
	visible := append([]string(nil), body[start:min(len(body), start+m.bodyHeight())]...)
	for len(visible) < m.bodyHeight() {
		visible = append(visible, "")
	}
	foot := "r refresh   q quit"
	if len(body) > m.bodyHeight() {
		foot = "↑↓ scroll   r refresh   q quit"
		if ansi.StringWidth(foot) > width {
			foot = "↑↓  r refresh  q quit"
		}
	}
	rows := append([]string{header, ""}, visible...)
	rows = append(rows, "", m.style(foot, "muted"))
	pad := strings.Repeat(" ", m.padding())
	for i, line := range rows {
		rows[i] = pad + ansi.Truncate(line, width, "…")
	}
	v := tea.NewView(strings.Join(rows, "\n"))
	v.AltScreen = true
	return v
}

func (m *Model) body() []string {
	width := m.innerWidth()
	var lines []string
	for i, s := range m.states {
		if i > 0 {
			lines = append(lines, "")
		}
		name := strings.ToUpper(s.provider.ID())
		status, kind := "◌ connecting", "muted"
		if s.snapshot != nil {
			status, kind = "● live · "+s.snapshot.ObservedAt.Local().Format("15:04"), "good"
			if s.err != nil {
				status, kind = "! stale · "+age(m.now.Sub(s.snapshot.ObservedAt))+" old", "warn"
			}
			if s.fetching && s.err == nil {
				status, kind = "◌ updating", "muted"
			}
		} else if s.err != nil {
			status, kind = "! unavailable", "warn"
		}
		left := m.style(name, s.provider.ID())
		right := m.style(status, kind)
		lines = append(lines, left+strings.Repeat(" ", max(1, width-ansi.StringWidth(left)-ansi.StringWidth(right)))+right)
		if s.snapshot != nil {
			labelWidth := 2
			for _, w := range s.snapshot.Windows {
				labelWidth = max(labelWidth, ansi.StringWidth(w.Label))
			}
			labelWidth = min(labelWidth, min(18, max(8, width/3)))
			for _, w := range s.snapshot.Windows {
				lines = append(lines, m.window(w, width, labelWidth)...)
			}
			for _, detail := range s.snapshot.Details {
				lines = append(lines, m.wrapped(detail, "muted", width)...)
			}
			for _, notice := range s.snapshot.Notices {
				lines = append(lines, m.wrapped("! "+notice, "warn", width)...)
			}
		}
		if s.err != nil {
			lines = append(lines, m.wrapped(s.err.Error(), "warn", width)...)
			if m.now.Before(s.nextPoll) {
				lines = append(lines, m.wrapped("Retry in "+countdown(s.nextPoll.Sub(m.now)), "muted", width)...)
			}
		}
	}
	return lines
}

func (m *Model) wrapped(text, kind string, width int) []string {
	var lines []string
	for _, line := range strings.Split(ansi.Wrap(text, max(1, width-2), ""), "\n") {
		lines = append(lines, "  "+m.style(line, kind))
	}
	return lines
}

func (m *Model) window(w provider.Window, width, labelWidth int) []string {
	kind := "good"
	if w.UsedPercent >= 90 {
		kind = "bad"
	} else if w.UsedPercent >= 70 {
		kind = "warn"
	}
	label := ansi.Truncate(w.Label, labelWidth, "…")
	label += strings.Repeat(" ", max(0, labelWidth-ansi.StringWidth(label)))
	pct := fmt.Sprintf("%3.0f%% used", w.UsedPercent)
	reset := "reset time unavailable"
	if w.ResetsAt != nil {
		if !w.ResetsAt.After(m.now) {
			reset = "reset pending"
		} else {
			reset = "resets in " + countdown(w.ResetsAt.Sub(m.now))
		}
	}
	available := width - 2
	barWidth := min(20, available-labelWidth-ansi.StringWidth(pct)-22-6)
	if barWidth >= 8 {
		return []string{"  " + label + "  " + m.bar(barWidth, w.UsedPercent, kind) + "  " + m.style(pct, kind) + "  " + m.style(reset, "muted")}
	}
	barWidth = min(16, available-labelWidth-ansi.StringWidth(pct)-4)
	line := "  " + label + "  "
	if barWidth >= 4 {
		line += m.bar(barWidth, w.UsedPercent, kind) + "  "
	}
	line += m.style(pct, kind)
	return []string{line, "    " + m.style(reset, "muted")}
}

func (m *Model) bar(width int, used float64, kind string) string {
	filled := int(math.Round(float64(width) * min(100, max(0, used)) / 100))
	return m.style(strings.Repeat("█", filled), kind) + m.style(strings.Repeat("░", width-filled), "muted")
}

func countdown(d time.Duration) string {
	minutes := max(1, int(math.Ceil(d.Minutes())))
	if minutes >= 24*60 {
		return fmt.Sprintf("%dd %dh", minutes/(24*60), (minutes%(24*60))/60)
	}
	if minutes >= 60 {
		return fmt.Sprintf("%dh %02dm", minutes/60, minutes%60)
	}
	return fmt.Sprintf("%dm", minutes)
}

func age(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", max(0, int(d.Seconds())))
	}
	return countdown(d)
}

func refreshDuration(d time.Duration) string {
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	return d.String()
}
