package dashboard

import (
	"context"
	"errors"
	"math/rand/v2"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/cyakimov/metron/internal/provider"
)

type state struct {
	provider   provider.Provider
	interval   time.Duration
	snapshot   *provider.Snapshot
	err        error
	fetching   bool
	nextPoll   time.Time
	healthFrom map[string]float64
	healthAt   time.Time
	reactions  map[string]reaction
}

type Model struct {
	states                []state
	ctx                   context.Context
	cancel                context.CancelFunc
	now                   time.Time
	width, height, offset int
	dark, color           bool
	motion                bool
	animationAt           time.Time
	animationPending      bool
	nextBlink, blinkUntil time.Time
	successUntil          time.Time
	sceneAt               time.Time
	stormTier             int
	stormAt, nextStorm    time.Time
	random                *rand.Rand
	nextVisit             time.Time
	visit                 ambientVisit
}

type tickMsg time.Time
type resultMsg struct {
	index    int
	snapshot provider.Snapshot
	err      error
	at       time.Time
}

func New(providers []provider.Provider, intervals map[string]time.Duration, motionEnabled bool) *Model {
	ctx, cancel := context.WithCancel(context.Background())
	now := time.Now()
	m := &Model{
		ctx: ctx, cancel: cancel, now: now,
		width: 80, height: 24, dark: true,
		color: os.Getenv("NO_COLOR") == "", motion: motionEnabled,
		animationAt: now, nextBlink: now.Add(6 * time.Second), sceneAt: now,
		random: rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())),
	}
	for _, p := range providers {
		m.states = append(m.states, state{provider: p, interval: intervals[p.ID()]})
	}
	return m
}

func (m *Model) Close() { m.cancel() }

func (m *Model) Init() tea.Cmd {
	commands := []tea.Cmd{tick(), tea.RequestBackgroundColor}
	for i := range m.states {
		commands = append(commands, m.poll(i))
	}
	m.updateAmbient()
	commands = append(commands, m.animate())
	return tea.Batch(commands...)
}

func tick() tea.Cmd { return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }) }

func (m *Model) poll(i int) tea.Cmd {
	s := &m.states[i]
	if s.fetching {
		return nil
	}
	s.fetching = true
	p := s.provider
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, provider.Timeout)
		defer cancel()
		snapshot, err := p.Fetch(ctx)
		return resultMsg{index: i, snapshot: snapshot, err: err, at: time.Now()}
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampOffset()
	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
	case tea.ColorProfileMsg:
		if msg.Profile == colorprofile.NoTTY || msg.Profile == colorprofile.Ascii {
			m.color = false
		}
	case resultMsg:
		m.now = msg.at
		m.advanceAnimation(msg.at)
		s := &m.states[msg.index]
		s.fetching = false
		s.nextPoll = msg.at.Add(s.interval)
		if msg.err == nil {
			m.acceptSnapshot(s, msg.snapshot)
			m.successUntil = msg.at.Add(750 * time.Millisecond)
		} else {
			var problem *provider.Problem
			if errors.As(msg.err, &problem) && problem.RetryAt.After(s.nextPoll) {
				s.nextPoll = problem.RetryAt
			}
		}
		s.err = msg.err
		m.updateStorm()
		m.clampOffset()
	case tickMsg:
		m.now = time.Time(msg)
		m.advanceAnimation(m.now)
		if !m.now.Before(m.nextBlink) {
			m.blinkUntil = m.now.Add(250 * time.Millisecond)
			m.nextBlink = m.now.Add(6 * time.Second)
		}
		m.updateStorm()
		m.updateAmbient()
		commands := []tea.Cmd{tick()}
		for i := range m.states {
			if !m.states[i].fetching && !m.now.Before(m.states[i].nextPoll) {
				commands = append(commands, m.poll(i))
			}
		}
		commands = append(commands, m.animate())
		return m, tea.Batch(commands...)
	case animationMsg:
		m.animationPending = false
		m.advanceAnimation(time.Time(msg))
		m.updateStorm()
		m.updateAmbient()
		return m, m.animate()
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.cancel()
			return m, tea.Quit
		case "r":
			m.now = time.Now()
			m.advanceAnimation(m.now)
			var commands []tea.Cmd
			for i := range m.states {
				var problem *provider.Problem
				if errors.As(m.states[i].err, &problem) && m.now.Before(problem.RetryAt) {
					continue
				}
				commands = append(commands, m.poll(i))
			}
			commands = append(commands, m.animate())
			return m, tea.Batch(commands...)
		case "a":
			m.motion = !m.motion
			m.advanceAnimation(time.Now())
			m.blinkUntil, m.successUntil = time.Time{}, time.Time{}
			m.nextBlink = m.animationAt.Add(6 * time.Second)
			m.sceneAt = m.animationAt
			m.visit, m.nextVisit = ambientVisit{}, time.Time{}
			m.stormTier, m.stormAt, m.nextStorm = 0, time.Time{}, time.Time{}
			m.updateStorm()
			for i := range m.states {
				m.states[i].healthFrom = nil
				m.states[i].reactions = nil
			}
		case "down", "j":
			m.offset++
		case "up", "k":
			m.offset--
		case "pgdown":
			m.offset += m.bodyHeight()
		case "pgup":
			m.offset -= m.bodyHeight()
		case "home":
			m.offset = 0
		case "end":
			m.offset = len(m.body())
		}
		m.clampOffset()
	}
	m.updateAmbient()
	return m, m.animate()
}

func (m *Model) expanded() bool { return m.width >= 60 && m.height >= 16 }
func (m *Model) bodyHeight() int {
	if m.expanded() {
		return max(1, m.height-7)
	}
	return max(1, m.height-4)
}
func (m *Model) clampOffset() { m.offset = max(0, min(m.offset, max(0, len(m.body())-m.bodyHeight()))) }
