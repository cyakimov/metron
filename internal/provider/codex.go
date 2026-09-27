package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Codex struct {
	mu      sync.Mutex
	client  *rpcClient
	version string
}

func NewCodex(version string) *Codex { return &Codex{version: version} }
func (*Codex) ID() string            { return "codex" }

func (c *Codex) Fetch(ctx context.Context) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if c.client == nil {
		client, err := startRPC()
		if err != nil {
			return Snapshot{}, &Problem{Message: "Install Codex and sign in with codex login"}
		}
		c.client = client
		_, err = client.call(ctx, "initialize", map[string]any{
			"clientInfo":   map[string]string{"name": "metron", "title": "Metron", "version": c.version},
			"capabilities": map[string]bool{"explicitGatewayOauth": true},
		})
		if err == nil {
			err = client.notify("initialized", map[string]any{})
		}
		if err != nil {
			c.reset()
			return Snapshot{}, &Problem{Message: "Could not initialize Codex; update the Codex CLI"}
		}
	}
	raw, err := c.client.call(ctx, "account/rateLimits/read", map[string]any{})
	if err != nil {
		c.reset()
		return Snapshot{}, &Problem{Message: "Could not read Codex usage; check codex login"}
	}
	snapshot, err := parseCodex(raw)
	if err != nil {
		return Snapshot{}, &Problem{Message: "Codex returned an unfamiliar usage response"}
	}
	snapshot.ObservedAt = time.Now()
	return snapshot, nil
}

func (c *Codex) reset() {
	if c.client != nil {
		c.client.close()
		c.client = nil
	}
}

func (c *Codex) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reset()
}

type codexWindow struct {
	Used    *float64 `json:"usedPercent"`
	Minutes *int64   `json:"windowDurationMins"`
	Reset   *int64   `json:"resetsAt"`
}

type codexLimit struct {
	ID        string       `json:"limitId"`
	Name      string       `json:"limitName"`
	Model     string       `json:"normalModelSlug"`
	Primary   *codexWindow `json:"primary"`
	Secondary *codexWindow `json:"secondary"`
	Credits   *struct {
		Balance    *string `json:"balance"`
		HasCredits bool    `json:"hasCredits"`
		Unlimited  bool    `json:"unlimited"`
	} `json:"credits"`
	Spend *struct {
		Used      string  `json:"used"`
		Limit     string  `json:"limit"`
		Remaining float64 `json:"remainingPercent"`
		Reset     int64   `json:"resetsAt"`
	} `json:"individualLimit"`
	SpendReached bool   `json:"spendControlReached"`
	Reached      string `json:"rateLimitReachedType"`
}

func parseCodex(raw []byte) (Snapshot, error) {
	var response struct {
		Aggregate *codexLimit           `json:"rateLimits"`
		ByID      map[string]codexLimit `json:"rateLimitsByLimitId"`
		Allowed   *bool                 `json:"ordinaryUsageAllowed"`
		Resets    *struct {
			Count int `json:"availableCount"`
		} `json:"rateLimitResetCredits"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || response.Aggregate == nil {
		return Snapshot{}, fmt.Errorf("missing rate limits")
	}
	if len(response.ByID) == 0 {
		response.ByID = map[string]codexLimit{response.Aggregate.ID: *response.Aggregate}
	}
	keys := make([]string, 0, len(response.ByID))
	for key := range response.ByID {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var snapshot Snapshot
	seen := make(map[string]bool)
	addDetail := func(line string) {
		if !seen[line] {
			seen[line] = true
			snapshot.Details = append(snapshot.Details, line)
		}
	}
	for _, key := range keys {
		limit := response.ByID[key]
		scope := ""
		if key != "codex" && key != "" {
			scope = limit.Name
			if scope == "" {
				scope = limit.Model
			}
			if scope == "" {
				scope = humanize(key)
			}
		}
		for i, w := range []*codexWindow{limit.Primary, limit.Secondary} {
			if w == nil || w.Used == nil {
				continue
			}
			d := time.Duration(0)
			if w.Minutes != nil && *w.Minutes > 0 && *w.Minutes <= 525600 {
				d = time.Duration(*w.Minutes) * time.Minute
			}
			label := durationLabel(d)
			if scope != "" {
				label = scope + " " + label
			}
			var reset *time.Time
			if w.Reset != nil {
				t := time.Unix(*w.Reset, 0)
				reset = &t
			}
			snapshot.Windows = append(snapshot.Windows, Window{ID: fmt.Sprintf("%s:%d", key, i), Label: label, UsedPercent: *w.Used, Duration: d, ResetsAt: reset})
		}
		if limit.Credits != nil {
			switch {
			case limit.Credits.Unlimited:
				addDetail("Credits unlimited")
			case limit.Credits.Balance != nil:
				addDetail("Credits  " + *limit.Credits.Balance + " left")
			case limit.Credits.HasCredits:
				addDetail("Credits available")
			default:
				addDetail("Credits none")
			}
		}
		if limit.Spend != nil {
			t := time.Unix(limit.Spend.Reset, 0)
			label := "Spend limit"
			if scope != "" {
				label = scope + " spend"
			}
			snapshot.Windows = append(snapshot.Windows, Window{ID: key + ":spend", Label: label, UsedPercent: 100 - limit.Spend.Remaining, ResetsAt: &t})
			addDetail("Spend  " + limit.Spend.Used + " used of " + limit.Spend.Limit)
		}
		if limit.SpendReached {
			snapshot.Notices = append(snapshot.Notices, "Spend limit reached")
		}
		if limit.Reached != "" {
			notice := strings.ReplaceAll(limit.Reached, "_", " ")
			snapshot.Notices = append(snapshot.Notices, humanize(notice))
		}
	}
	if response.Allowed != nil && !*response.Allowed {
		snapshot.Notices = append(snapshot.Notices, "Included usage unavailable")
	}
	if response.Resets != nil && response.Resets.Count > 0 {
		addDetail(fmt.Sprintf("Free resets  %d available", response.Resets.Count))
	}
	return normalize(snapshot), nil
}
