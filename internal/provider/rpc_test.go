package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestRPCProcess(t *testing.T) {
	mode := os.Getenv("METRON_TEST_RPC")
	if mode == "" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     *uint64 `json:"id"`
			Method string  `json:"method"`
		}
		json.Unmarshal(scanner.Bytes(), &request)
		if request.ID == nil {
			continue
		}
		if mode == "hang" {
			time.Sleep(time.Hour)
			return
		}
		if mode == "exit" {
			os.Exit(0)
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.Encode(map[string]any{"method": "account/rateLimits/updated", "params": map[string]any{}})
		encoder.Encode(map[string]any{"id": *request.ID, "result": map[string]any{"rateLimits": map[string]any{"primary": map[string]any{"usedPercent": 27, "windowDurationMins": 10080}}}})
	}
	os.Exit(0)
}

func fakeRPC(t *testing.T, mode string) *rpcClient {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRPCProcess$")
	cmd.Env = append(os.Environ(), "METRON_TEST_RPC="+mode)
	client, err := startRPCCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.close)
	return client
}

func TestRPCIgnoresNotificationsAndKeepsConnection(t *testing.T) {
	client := fakeRPC(t, "respond")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for i := 0; i < 3; i++ {
		raw, err := client.call(ctx, "account/rateLimits/read", map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		s, err := parseCodex(raw)
		if err != nil || len(s.Windows) != 1 || s.Windows[0].UsedPercent != 27 {
			t.Fatal(s, err)
		}
	}
}

func TestRPCStopsAfterTimeoutOrDisconnect(t *testing.T) {
	for _, mode := range []string{"hang", "exit"} {
		t.Run(mode, func(t *testing.T) {
			client := fakeRPC(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			if _, err := client.call(ctx, "initialize", map[string]any{}); err == nil {
				t.Fatal("expected failure")
			}
		})
	}
}
