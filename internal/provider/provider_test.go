package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestClaudeCurrentResponseUsesNamedLimits(t *testing.T) {
	s, err := parseClaude(fixture(t, "claude-modern.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Windows) != 3 {
		t.Fatalf("duplicate or missing windows: %+v", s.Windows)
	}
	labels := []string{s.Windows[0].Label, s.Windows[1].Label, s.Windows[2].Label}
	if !slices.Equal(labels, []string{"5h", "7d", "Fable 7d"}) {
		t.Fatal(labels)
	}
	if s.Windows[0].UsedPercent != 100 || s.Windows[1].UsedPercent != 59 {
		t.Fatal(s.Windows)
	}
	if s.Windows[0].ResetsAt == nil || s.Windows[0].ResetsAt.Nanosecond() != 197442000 {
		t.Fatal("reset precision lost")
	}
}

func TestClaudeLegacyAndCredits(t *testing.T) {
	s, err := parseClaude(fixture(t, "claude-legacy.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Windows) != 4 {
		t.Fatal(s.Windows)
	}
	if s.Windows[1].Label != "7d" || s.Windows[1].ResetsAt != nil {
		t.Fatal("missing reset was invented")
	}
	if !slices.Contains(s.Details, "Extra usage  USD 2.50 used of USD 10.00") {
		t.Fatal(s.Details)
	}
}

func TestClaudeScopesAndUnknownWindows(t *testing.T) {
	s, err := parseClaude([]byte(`{"limits":[{"kind":"weekly_scoped","percent":12,"scope":{"model":{"display_name":"Opus"}}},{"kind":"weekly_scoped","percent":12,"scope":{"model":{"display_name":"Opus"}}},{"kind":"monthly","percent":40,"resets_at":"bad-date"} ]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Windows) != 2 || s.Windows[0].Label != "Opus 7d" || s.Windows[1].Label != "Monthly" || s.Windows[1].ResetsAt != nil {
		t.Fatal(s.Windows)
	}
}

func TestCodexUsesMultiBucketWithoutInventingFiveHour(t *testing.T) {
	s, err := parseCodex(fixture(t, "codex-multi.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Windows) != 3 {
		t.Fatal(s.Windows)
	}
	if s.Windows[0].Label != "Spark 5h" || s.Windows[1].Label != "7d" || s.Windows[2].UsedPercent != 25 {
		t.Fatal(s.Windows)
	}
	if len(s.Details) != 3 || !slices.Contains(s.Details, "Credits  15 left") {
		t.Fatal(s.Details)
	}
	if !slices.Contains(s.Notices, "Included usage unavailable") {
		t.Fatal(s.Notices)
	}
}

func TestClaudeModelAndSurfaceStayDistinct(t *testing.T) {
	s, err := parseClaude([]byte(`{"limits":[{"kind":"weekly_scoped","percent":12,"scope":{"model":{"display_name":"Opus"},"surface":"Code"}},{"kind":"weekly_scoped","percent":30,"scope":{"model":{"display_name":"Opus"},"surface":{"display_name":"Web"}}}]}`))
	if err != nil || len(s.Windows) != 2 || s.Windows[0].Label != "Opus Code 7d" || s.Windows[1].Label != "Opus Web 7d" {
		t.Fatal(s, err)
	}
}

func TestClaudeModernSpendRetainsExtraWindows(t *testing.T) {
	s, err := parseClaude([]byte(`{"spend":{"enabled":true,"used":{"amount_minor":250,"currency":"USD","exponent":2},"limit":{"amount_minor":1000,"currency":"USD","exponent":2},"balance":{"amount_minor":750,"currency":"USD","exponent":2},"percent":25},"extra_usage":{"is_enabled":true,"utilization":25,"used_credits":250,"monthly_limit":1000,"daily":{"utilization":50,"resets_at":"2030-01-02T00:00:00Z"},"weekly":{"utilization":20,"resets_at":"2030-01-08T00:00:00Z"}}}`))
	if err != nil || len(s.Windows) != 3 || len(s.Details) != 2 {
		t.Fatal(s, err)
	}
	if !slices.Contains(s.Details, "Credits  USD 7.50 left") || !slices.Contains(s.Details, "Extra usage  USD 2.50 used of USD 10.00") {
		t.Fatal(s.Details)
	}
	if s.Windows[0].Label != "Extra 1d" || s.Windows[1].Label != "Extra 7d" || s.Windows[2].UsedPercent != 25 {
		t.Fatal(s.Windows)
	}
}

func TestCodexFallbackUnlimitedAndMissingReset(t *testing.T) {
	s, err := parseCodex([]byte(`{"rateLimits":{"primary":{"usedPercent":0,"windowDurationMins":10080},"credits":{"unlimited":true,"hasCredits":true}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Windows) != 1 || s.Windows[0].ResetsAt != nil || s.Windows[0].Label != "7d" || s.Details[0] != "Credits unlimited" {
		t.Fatal(s)
	}
}

func TestMalformedResponses(t *testing.T) {
	for _, raw := range []string{`{`, `null`, `{}`, `[]`} {
		if _, err := parseClaude([]byte(raw)); err == nil {
			t.Errorf("Claude accepted %s", raw)
		}
		if _, err := parseCodex([]byte(raw)); err == nil {
			t.Errorf("Codex accepted %s", raw)
		}
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (fn transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestClaudeStaleCredentialFallbackAndHeaders(t *testing.T) {
	calls := 0
	c := NewClaude()
	c.credentials = func(context.Context) []string { return []string{"expired", "fresh"} }
	c.client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("anthropic-beta") != "oauth-2025-04-20" {
			t.Error("OAuth header missing")
		}
		if calls == 1 {
			return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("secret-error"))}, nil
		}
		if r.Header.Get("Authorization") != "Bearer fresh" {
			t.Error("wrong credential")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(fixture(t, "claude-modern.json"))))}, nil
	})}
	s, err := c.Fetch(context.Background())
	if err != nil || calls != 2 || s.ObservedAt.IsZero() {
		t.Fatalf("calls=%d snapshot=%+v error=%v", calls, s, err)
	}
}

func TestClaudeFailuresAreSafeAndDoNotRetryOtherCredentials(t *testing.T) {
	for _, status := range []int{429, 503, 200} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			c := NewClaude()
			c.credentials = func(context.Context) []string { return []string{"credential-one", "credential-two"} }
			c.client = &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{"120"}}, Body: io.NopCloser(strings.NewReader("sensitive-server-response"))}, nil
			})}
			_, err := c.Fetch(context.Background())
			if err == nil || calls != 1 || strings.Contains(err.Error(), "sensitive") {
				t.Fatal(calls, err)
			}
			var problem *Problem
			if status == 429 && (!errors.As(err, &problem) || time.Until(problem.RetryAt) < 119*time.Second) {
				t.Fatal(err)
			}
		})
	}
}

func TestClaudeMissingAndExpiredAuthentication(t *testing.T) {
	c := NewClaude()
	c.credentials = func(context.Context) []string { return nil }
	if _, err := c.Fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "claude auth login") {
		t.Fatal(err)
	}
	c.credentials = func(context.Context) []string { return []string{"expired"} }
	c.client = &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	if _, err := c.Fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatal(err)
	}
}

func TestCredentialOverrideAndSiblingTokens(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", " explicit-token ")
	if got := claudeCredentials(context.Background()); !slices.Equal(got, []string{"explicit-token"}) {
		t.Fatal(got)
	}
	if got := claudeToken([]byte(`{"mcpOAuth":{"accessToken":"wrong"},"claudeAiOauth":{"accessToken":"right"}}`)); got != "right" {
		t.Fatal(got)
	}
	if got := claudeToken([]byte(`{"mcpOAuth":{"accessToken":"wrong"}}`)); got != "" {
		t.Fatal(got)
	}
}

func TestLabelsCannotInjectTerminalControls(t *testing.T) {
	s := normalize(Snapshot{Windows: []Window{{ID: "a", Label: "Fable\x1b[31m\n\x9b", UsedPercent: 0}}})
	if strings.ContainsAny(s.Windows[0].Label, "\x1b\n\x9b") {
		t.Fatal(s.Windows[0].Label)
	}
}
