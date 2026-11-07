package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/cyakimov/metron/internal/provider"
)

type request struct {
	Version  int    `json:"version"`
	Provider string `json:"provider"`
}
type response struct {
	Version  int                `json:"version"`
	Provider string             `json:"provider"`
	Snapshot *provider.Snapshot `json:"snapshot,omitempty"`
	Error    string             `json:"error,omitempty"`
	RetryAt  *time.Time         `json:"retryAt,omitempty"`
}

func main() {
	codex := provider.NewCodex("mac-dev")
	defer codex.Close()
	providers := map[string]provider.Provider{"claude": provider.NewClaude(), "codex": codex}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req request
		res := response{Version: 1}
		if json.Unmarshal(scanner.Bytes(), &req) != nil || req.Version != 1 || providers[req.Provider] == nil {
			res.Error = "Unsupported request"
		} else {
			res.Provider = req.Provider
			ctx, cancel := context.WithTimeout(context.Background(), provider.Timeout)
			snapshot, err := providers[req.Provider].Fetch(ctx)
			cancel()
			if err != nil {
				res.Error = "Usage unavailable"
				var problem *provider.Problem
				if errors.As(err, &problem) {
					res.Error = problem.Message
					if !problem.RetryAt.IsZero() {
						res.RetryAt = &problem.RetryAt
					}
				}
			} else {
				res.Snapshot = &snapshot
			}
		}
		if encoder.Encode(res) != nil {
			return
		}
	}
}
