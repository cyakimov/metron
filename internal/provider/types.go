package provider

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const Timeout = 15 * time.Second

type Provider interface {
	ID() string
	Fetch(context.Context) (Snapshot, error)
}

type Window struct {
	ID          string
	Label       string
	UsedPercent float64
	Duration    time.Duration
	ResetsAt    *time.Time
}

type Snapshot struct {
	Windows    []Window
	Details    []string
	Notices    []string
	ObservedAt time.Time
}

type Problem struct {
	Message string
	RetryAt time.Time
}

func (p *Problem) Error() string { return p.Message }

func normalize(snapshot Snapshot) Snapshot {
	windows := make([]Window, 0, len(snapshot.Windows))
	seen := make(map[string]bool)
	for _, w := range snapshot.Windows {
		if math.IsNaN(w.UsedPercent) || math.IsInf(w.UsedPercent, 0) || w.UsedPercent < 0 || seen[w.ID] {
			continue
		}
		seen[w.ID] = true
		w.Label = cleanLabel(w.Label)
		windows = append(windows, w)
	}
	sort.SliceStable(windows, func(i, j int) bool {
		a, b := windows[i], windows[j]
		if a.Duration != b.Duration {
			if a.Duration == 0 {
				return false
			}
			if b.Duration == 0 {
				return true
			}
			return a.Duration < b.Duration
		}
		if a.Label == "7d" && b.Label != "7d" {
			return true
		}
		if b.Label == "7d" && a.Label != "7d" {
			return false
		}
		return a.Label < b.Label
	})
	snapshot.Windows = windows
	for i := range snapshot.Details {
		snapshot.Details[i] = cleanLabel(snapshot.Details[i])
	}
	for i := range snapshot.Notices {
		snapshot.Notices[i] = cleanLabel(snapshot.Notices[i])
	}
	if len(snapshot.Windows) == 0 && len(snapshot.Details) == 0 && len(snapshot.Notices) == 0 {
		snapshot.Notices = []string{"No account limits reported"}
	}
	return snapshot
}

// Provider labels are untrusted terminal input, including C1 control sequences.
func cleanLabel(s string) string {
	s = strings.ToValidUTF8(s, "")
	return strings.Map(func(r rune) rune {
		if r < 32 || (r >= 127 && r <= 159) {
			return -1
		}
		return r
	}, s)
}

func durationLabel(d time.Duration) string {
	if d <= 0 {
		return "Window"
	}
	if d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("%dm", int(d/time.Minute))
}

func timestamp(s string) *time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil
	}
	return &t
}
