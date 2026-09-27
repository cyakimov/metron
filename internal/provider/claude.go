package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const claudeURL = "https://api.anthropic.com/api/oauth/usage"

type Claude struct {
	client      *http.Client
	url         string
	credentials func(context.Context) []string
}

func NewClaude() *Claude {
	return &Claude{client: &http.Client{Timeout: Timeout}, url: claudeURL, credentials: claudeCredentials}
}

func (*Claude) ID() string { return "claude" }

func (c *Claude) Fetch(ctx context.Context) (Snapshot, error) {
	tokens := c.credentials(ctx)
	if len(tokens) == 0 {
		return Snapshot{}, &Problem{Message: "Sign in with claude auth login"}
	}
	for _, token := range tokens {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
		if err != nil {
			return Snapshot{}, &Problem{Message: "Could not prepare Claude usage request"}
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("anthropic-beta", "oauth-2025-04-20")
		req.Header.Set("User-Agent", "metron")
		resp, err := c.client.Do(req)
		if err != nil {
			return Snapshot{}, &Problem{Message: "Could not reach Claude usage"}
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			continue
		}
		if resp.StatusCode == 429 {
			return Snapshot{}, &Problem{Message: "Claude asked to slow down", RetryAt: retryAfter(resp.Header.Get("Retry-After"), time.Now())}
		}
		if resp.StatusCode != 200 {
			return Snapshot{}, &Problem{Message: fmt.Sprintf("Claude usage unavailable (HTTP %d)", resp.StatusCode)}
		}
		if readErr != nil {
			return Snapshot{}, &Problem{Message: "Could not read Claude usage"}
		}
		snapshot, err := parseClaude(body)
		if err != nil {
			return Snapshot{}, &Problem{Message: "Claude returned an unfamiliar usage response"}
		}
		snapshot.ObservedAt = time.Now()
		return normalize(snapshot), nil
	}
	return Snapshot{}, &Problem{Message: "Claude login expired; run claude auth login"}
}

func claudeCredentials(ctx context.Context) []string {
	if token := strings.TrimSpace(os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")); token != "" {
		return []string{token}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		dir = filepath.Join(home, ".claude")
	}
	var tokens []string
	if blob, err := os.ReadFile(filepath.Join(dir, ".credentials.json")); err == nil {
		if token := claudeToken(blob); token != "" {
			tokens = append(tokens, token)
		}
	}
	// A custom config directory may belong to a different account.
	if filepath.Clean(dir) == filepath.Join(home, ".claude") {
		if blob, err := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-w", "-s", "Claude Code-credentials").Output(); err == nil {
			if token := claudeToken(blob); token != "" && (len(tokens) == 0 || tokens[0] != token) {
				tokens = append(tokens, token)
			}
		}
	}
	return tokens
}

func claudeToken(blob []byte) string {
	var data struct {
		OAuth struct {
			Access string `json:"accessToken"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(blob, &data) != nil {
		return ""
	}
	return strings.TrimSpace(data.OAuth.Access)
}

type claudeLimit struct {
	Kind    string   `json:"kind"`
	Group   string   `json:"group"`
	Percent *float64 `json:"percent"`
	Reset   string   `json:"resets_at"`
	Scope   *struct {
		Model *struct {
			ID   string `json:"id"`
			Name string `json:"display_name"`
		} `json:"model"`
		Surface json.RawMessage `json:"surface"`
	} `json:"scope"`
}

func parseClaude(body []byte) (Snapshot, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return Snapshot{}, fmt.Errorf("invalid object")
	}
	var snapshot Snapshot
	var limits []claudeLimit
	if err := json.Unmarshal(fields["limits"], &limits); err == nil {
		for _, limit := range limits {
			if limit.Percent == nil {
				continue
			}
			label, duration := limit.Kind, time.Duration(0)
			switch limit.Kind {
			case "session", "five_hour":
				label, duration = "5h", 5*time.Hour
			case "weekly_all", "seven_day":
				label, duration = "7d", 7*24*time.Hour
			case "weekly_scoped":
				label, duration = "Scoped 7d", 7*24*time.Hour
			default:
				label = humanize(limit.Kind)
			}
			scope := ""
			if limit.Scope != nil {
				if limit.Scope.Model != nil {
					scope = limit.Scope.Model.Name
					if scope == "" {
						scope = limit.Scope.Model.ID
					}
				}
				surface := ""
				json.Unmarshal(limit.Scope.Surface, &surface)
				if surface == "" {
					var s struct {
						Name string `json:"display_name"`
						ID   string `json:"id"`
					}
					json.Unmarshal(limit.Scope.Surface, &s)
					surface = s.Name
					if surface == "" {
						surface = s.ID
					}
				}
				if surface != "" {
					scope = strings.TrimSpace(scope + " " + surface)
				}
			}
			if scope != "" {
				label = scope + " " + durationLabel(duration)
			}
			snapshot.Windows = append(snapshot.Windows, Window{ID: limit.Kind + ":" + strings.ToLower(scope), Label: label, Duration: duration, UsedPercent: *limit.Percent, ResetsAt: timestamp(limit.Reset)})
		}
	}
	// The modern list names opaque legacy buckets and is the canonical view.
	if len(snapshot.Windows) == 0 {
		for key, raw := range fields {
			if key == "extra_usage" {
				continue
			}
			var bucket struct {
				Used  *float64 `json:"utilization"`
				Reset string   `json:"resets_at"`
			}
			if json.Unmarshal(raw, &bucket) != nil || bucket.Used == nil {
				continue
			}
			label, d := humanize(key), time.Duration(0)
			if key == "five_hour" {
				label, d = "5h", 5*time.Hour
			}
			if key == "seven_day" {
				label, d = "7d", 7*24*time.Hour
			}
			if strings.HasPrefix(key, "seven_day_") {
				label, d = humanize(strings.TrimPrefix(key, "seven_day_"))+" 7d", 7*24*time.Hour
			}
			snapshot.Windows = append(snapshot.Windows, Window{ID: key, Label: label, Duration: d, UsedPercent: *bucket.Used, ResetsAt: timestamp(bucket.Reset)})
		}
	}
	parseClaudeSpend(fields, &snapshot)
	if len(snapshot.Windows) == 0 && len(snapshot.Details) == 0 {
		return Snapshot{}, fmt.Errorf("no usage fields")
	}
	return normalize(snapshot), nil
}

type money struct {
	Amount   float64 `json:"amount_minor"`
	Currency string  `json:"currency"`
	Exponent int     `json:"exponent"`
}

func (m money) String() string {
	if m.Exponent < 0 || m.Exponent > 6 || m.Currency == "" {
		return ""
	}
	return fmt.Sprintf("%s %.*f", m.Currency, m.Exponent, m.Amount/math.Pow10(m.Exponent))
}

func parseClaudeSpend(fields map[string]json.RawMessage, snapshot *Snapshot) {
	modern := false
	var spend struct {
		Enabled bool     `json:"enabled"`
		Used    *money   `json:"used"`
		Limit   *money   `json:"limit"`
		Balance *money   `json:"balance"`
		Percent *float64 `json:"percent"`
	}
	if raw, ok := fields["spend"]; ok && string(raw) != "null" && json.Unmarshal(raw, &spend) == nil {
		modern = true
		if spend.Balance != nil && spend.Balance.String() != "" {
			snapshot.Details = append(snapshot.Details, "Credits  "+spend.Balance.String()+" left")
		}
		if spend.Enabled && spend.Used != nil && spend.Used.String() != "" {
			line := "Extra usage  " + spend.Used.String() + " used"
			if spend.Limit != nil && spend.Limit.String() != "" {
				line += " of " + spend.Limit.String()
			}
			snapshot.Details = append(snapshot.Details, line)
			if spend.Limit != nil && spend.Percent != nil {
				snapshot.Windows = append(snapshot.Windows, Window{ID: "extra_usage", Label: "Extra usage", UsedPercent: *spend.Percent})
			}
		}
		if !spend.Enabled {
			snapshot.Details = append(snapshot.Details, "Extra usage disabled")
		}
	}
	var extra struct {
		Enabled  bool     `json:"is_enabled"`
		Used     *float64 `json:"used_credits"`
		Limit    *float64 `json:"monthly_limit"`
		Percent  *float64 `json:"utilization"`
		Currency string   `json:"currency"`
		Decimals *int     `json:"decimal_places"`
		Daily    *struct {
			Percent *float64 `json:"utilization"`
			Reset   string   `json:"resets_at"`
		} `json:"daily"`
		Weekly *struct {
			Percent *float64 `json:"utilization"`
			Reset   string   `json:"resets_at"`
		} `json:"weekly"`
	}
	if json.Unmarshal(fields["extra_usage"], &extra) != nil || !extra.Enabled {
		return
	}
	format := func(n float64) string {
		if extra.Decimals != nil && extra.Currency != "" {
			return (money{n, extra.Currency, *extra.Decimals}).String()
		}
		return fmt.Sprintf("%g credits", n)
	}
	if !modern && extra.Used != nil {
		line := "Extra usage  " + format(*extra.Used) + " used"
		if extra.Limit != nil {
			line += " of " + format(*extra.Limit)
		}
		snapshot.Details = append(snapshot.Details, line)
	}
	if !modern && extra.Percent != nil {
		snapshot.Windows = append(snapshot.Windows, Window{ID: "extra_usage", Label: "Extra usage", UsedPercent: *extra.Percent})
	}
	if extra.Daily != nil && extra.Daily.Percent != nil {
		snapshot.Windows = append(snapshot.Windows, Window{ID: "extra_daily", Label: "Extra 1d", Duration: 24 * time.Hour, UsedPercent: *extra.Daily.Percent, ResetsAt: timestamp(extra.Daily.Reset)})
	}
	if extra.Weekly != nil && extra.Weekly.Percent != nil {
		snapshot.Windows = append(snapshot.Windows, Window{ID: "extra_weekly", Label: "Extra 7d", Duration: 7 * 24 * time.Hour, UsedPercent: *extra.Weekly.Percent, ResetsAt: timestamp(extra.Weekly.Reset)})
	}
}

func humanize(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	if s == "" {
		return "Window"
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func retryAfter(value string, now time.Time) time.Time {
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 && seconds <= int64((365*24*time.Hour)/time.Second) {
		return now.Add(time.Duration(seconds) * time.Second)
	}
	if t, err := http.ParseTime(value); err == nil && t.After(now) {
		return t
	}
	return now.Add(time.Minute)
}
