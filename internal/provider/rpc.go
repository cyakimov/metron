package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
)

type rpcMessage struct {
	ID     *uint64         `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code int `json:"code"`
	} `json:"error"`
}

type rpcClient struct {
	cmd       *exec.Cmd
	in        io.WriteCloser
	encoder   *json.Encoder
	responses chan rpcMessage
	waited    chan struct{}
	stopped   chan struct{}
	nextID    uint64
}

func startRPC() (*rpcClient, error) {
	return startRPCCommand(exec.Command("codex", "app-server", "--stdio"))
}

func startRPCCommand(cmd *exec.Cmd) (*rpcClient, error) {
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		in.Close()
		out.Close()
		return nil, err
	}
	client := &rpcClient{cmd: cmd, in: in, encoder: json.NewEncoder(in), responses: make(chan rpcMessage, 8), waited: make(chan struct{}), stopped: make(chan struct{})}
	go func() {
		defer close(client.responses)
		defer close(client.waited)
		defer cmd.Wait()
		scanner := bufio.NewScanner(out)
		scanner.Buffer(make([]byte, 4096), 4<<20)
		for scanner.Scan() {
			var msg rpcMessage
			if json.Unmarshal(scanner.Bytes(), &msg) != nil || msg.ID == nil {
				continue
			}
			select {
			case client.responses <- msg:
			case <-client.stopped:
				return
			}
		}
	}()
	return client, nil
}

func (c *rpcClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.nextID++
	id := c.nextID
	if err := c.encoder.Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, fmt.Errorf("app-server disconnected")
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case msg, ok := <-c.responses:
			if !ok {
				return nil, fmt.Errorf("app-server disconnected")
			}
			if msg.ID == nil || *msg.ID != id {
				continue
			}
			if msg.Error != nil {
				return nil, fmt.Errorf("app-server rejected request (code %d)", msg.Error.Code)
			}
			if len(msg.Result) == 0 {
				return nil, fmt.Errorf("app-server returned no result")
			}
			return msg.Result, nil
		}
	}
}

func (c *rpcClient) notify(method string, params any) error {
	return c.encoder.Encode(map[string]any{"method": method, "params": params})
}

func (c *rpcClient) close() {
	close(c.stopped)
	c.in.Close()
	c.cmd.Process.Kill()
	<-c.waited
}
