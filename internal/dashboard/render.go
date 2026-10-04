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

var darkPalette = map[string]string{
	"title": "#83DDF4", "accent": "#BC9CFF", "border": "#52617C",
	"track": "#344159", "muted": "#96A3B8", "claude": "#F2B38D",
	"codex": "#83DDF4", "good": "#8CDBC0", "warn": "#FFD089", "bad": "#FF8E9B",
	"starDim": "#3E516E", "star": "#6E93B6", "starBright": "#9FC6E0",
}

var lightPalette = map[string]string{
	"title": "#00708D", "accent": "#6E48B6", "border": "#8C9AB0",
	"track": "#CDD5DE", "muted": "#566477", "claude": "#A8512A",
	"codex": "#00708D", "good": "#14724F", "warn": "#956000", "bad": "#AA2644",
	"starDim": "#A2ADBF", "star": "#6884A2", "starBright": "#356C8D",
}

func (m *Model) style(text, kind string) string {
	s := lipgloss.NewStyle()
	if kind == "title" || kind == "claude" || kind == "codex" {
		s = s.Bold(true)
	}
	if m.color {
		palette := darkPalette
		if !m.dark {
			palette = lightPalette
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

func (m *Model) header() []string {
	width := m.innerWidth()
	left := m.style("metron", "title")
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
	if m.expanded() {
		mascot := m.mascot()
		_, status, kind := m.mood()
		text := []string{
			left + "  " + m.style("/ account limits", "muted"),
			m.style("C O S M I C   O B S E R V A T O R Y", "accent"),
			right,
			m.style("● "+status, kind),
		}
		for i := range text {
			text[i] = mascot[i] + "  " + text[i]
		}
		return append(text, "")
	}
	header := left
	if width >= 32 {
		_, _, kind := m.mood()
		left = m.style("✧ ", kind) + left
		header = left
	}
	if ansi.StringWidth(left)+1+ansi.StringWidth(right) <= width {
		header += strings.Repeat(" ", width-ansi.StringWidth(left)-ansi.StringWidth(right)) + right
	}
	return []string{header, ""}
}

func (m *Model) footer(scroll bool) string {
	motion := "a pause"
	if !m.motion {
		motion = "a animate"
	}
	foot := "r refresh   " + motion + "   q quit"
	if scroll {
		foot = "↑↓ scroll   " + foot
	}
	if ansi.StringWidth(foot) > m.innerWidth() {
		foot = "r refresh a motion q quit"
		if scroll {
			foot = "↑↓  r refresh  a  q quit"
		}
	}
	return m.style(foot, "muted")
}

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
	v := tea.NewView(strings.Join(m.background(m.foregroundRows()), "\n"))
	v.AltScreen = true
	return v
}

func (m *Model) foregroundRows() []string {
	body := m.body()
	start := min(m.offset, max(0, len(body)-m.bodyHeight()))
	visible := append([]string(nil), body[start:min(len(body), start+m.bodyHeight())]...)
	for len(visible) < m.bodyHeight() {
		visible = append(visible, "")
	}
	rows := append(m.header(), visible...)
	rows = append(rows, "", m.footer(len(body) > m.bodyHeight()))
	pad := strings.Repeat(" ", m.padding())
	for i, line := range rows {
		rows[i] = pad + ansi.Truncate(line, m.innerWidth(), "…")
	}
	return rows
}

func (m *Model) body() []string {
	width := m.innerWidth()
	framed := m.expanded()
	contentWidth := width
	if framed {
		contentWidth -= 4
	}
	labelWidth, resetWidth := 2, 0
	for _, s := range m.states {
		if s.snapshot != nil {
			for _, w := range s.snapshot.Windows {
				labelWidth = max(labelWidth, ansi.StringWidth(w.Label))
				resetWidth = max(resetWidth, ansi.StringWidth(m.resetText(w)))
			}
		}
	}
	labelWidth = min(labelWidth, min(18, max(8, contentWidth/3)))
	var lines []string
	for i, s := range m.states {
		if i > 0 {
			lines = append(lines, "")
		}
		left := m.style(strings.ToUpper(s.provider.ID()), s.provider.ID())
		border := m.borderKind(s)
		status, kind := m.providerStatus(s)
		right := m.style(status, kind)
		if framed {
			rule := strings.Repeat("─", max(0, width-ansi.StringWidth(left)-ansi.StringWidth(right)-8))
			lines = append(lines, m.style("╭─ ", border)+left+m.style(" "+rule+" ", border)+right+m.style(" ─╮", border))
		} else if ansi.StringWidth(left)+1+ansi.StringWidth(right) <= width {
			lines = append(lines, left+strings.Repeat(" ", width-ansi.StringWidth(left)-ansi.StringWidth(right))+right)
		} else {
			lines = append(lines, left, right)
		}
		var content []string
		if s.snapshot != nil {
			for _, w := range s.snapshot.Windows {
				content = append(content, m.window(w, contentWidth, labelWidth, resetWidth, m.barValue(s, w))...)
			}
			for _, detail := range s.snapshot.Details {
				content = append(content, m.wrapped(detail, "muted", contentWidth)...)
			}
			for _, notice := range s.snapshot.Notices {
				content = append(content, m.wrapped("! "+notice, "warn", contentWidth)...)
			}
		}
		if s.err != nil {
			content = append(content, m.wrapped(s.err.Error(), "warn", contentWidth)...)
			if m.now.Before(s.nextPoll) {
				content = append(content, m.wrapped("Retry in "+countdown(s.nextPoll.Sub(m.now)), "muted", contentWidth)...)
			}
		}
		if s.snapshot == nil && s.err == nil {
			content = append(content, m.wrapped("Awaiting account limits...", "muted", contentWidth)...)
		}
		for _, line := range content {
			if framed {
				line = ansi.Truncate(line, contentWidth, "…")
				line = m.style("│ ", border) + line + strings.Repeat(" ", max(0, contentWidth-ansi.StringWidth(line))) + m.style(" │", border)
			}
			lines = append(lines, line)
		}
		if framed {
			lines = append(lines, m.style("╰"+strings.Repeat("─", width-2)+"╯", border))
		}
	}
	return lines
}

func (m *Model) providerStatus(s state) (string, string) {
	if s.err != nil {
		if s.snapshot != nil {
			return "! stale · " + age(m.now.Sub(s.snapshot.ObservedAt)) + " old", "warn"
		}
		return "! unavailable", "warn"
	}
	if s.fetching {
		if s.snapshot != nil {
			return m.spinner() + " updating", "accent"
		}
		return m.spinner() + " connecting", "accent"
	}
	if s.snapshot != nil {
		return "● live · " + s.snapshot.ObservedAt.Local().Format("15:04"), "good"
	}
	return "◌ connecting", "muted"
}

func (m *Model) wrapped(text, kind string, width int) []string {
	var lines []string
	for _, line := range strings.Split(ansi.Wrap(text, max(1, width-2), ""), "\n") {
		lines = append(lines, "  "+m.style(line, kind))
	}
	return lines
}

func (m *Model) resetText(w provider.Window) string {
	if w.ResetsAt == nil {
		return "reset time unavailable"
	}
	if !w.ResetsAt.After(m.now) {
		return "reset pending"
	}
	return "resets in " + countdown(w.ResetsAt.Sub(m.now))
}

func (m *Model) window(w provider.Window, width, labelWidth, resetWidth int, barUsed float64) []string {
	kind := "good"
	if w.UsedPercent >= 90 {
		kind = "bad"
	} else if w.UsedPercent >= 70 {
		kind = "warn"
	}
	label := ansi.Truncate(w.Label, labelWidth, "…")
	label += strings.Repeat(" ", max(0, labelWidth-ansi.StringWidth(label)))
	pct := fmt.Sprintf("%3.0f%% used", w.UsedPercent)
	reset := m.resetText(w)
	available := width - 2
	barWidth := min(24, available-labelWidth-ansi.StringWidth(pct)-resetWidth-6)
	if barWidth >= 8 {
		return []string{"  " + label + "  " + m.bar(barWidth, barUsed, kind) + "  " + m.style(pct, kind) + "  " + m.style(reset, "muted")}
	}
	barWidth = min(16, available-labelWidth-ansi.StringWidth(pct)-4)
	line := "  " + label + "  "
	if barWidth >= 4 {
		line += m.bar(barWidth, barUsed, kind) + "  "
	}
	line += m.style(pct, kind)
	return []string{line, "    " + m.style(reset, "muted")}
}

func (m *Model) bar(width int, used float64, kind string) string {
	units := int(math.Round(float64(width*8) * min(100, max(0, used)) / 100))
	filled := units / 8
	bar := strings.Repeat("█", filled)
	if part := units % 8; part > 0 {
		bar += []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}[part]
		filled++
	}
	return m.style(bar, kind) + m.style(strings.Repeat("░", width-filled), "track")
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
