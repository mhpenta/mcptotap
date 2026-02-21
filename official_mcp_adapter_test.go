package mcptotap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhpenta/mcptotap"
)

func setup(t *testing.T) *mcptotap.Client {
	t.Helper()

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "test-server",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "greet",
		Description: "Greet someone by name",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input struct {
		Name string `json:"name"`
	}) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Hello, %s!", input.Name)},
			},
		}, nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "add",
		Description: "Add two numbers",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input struct {
		A int `json:"a"`
		B int `json:"b"`
	}) (*mcp.CallToolResult, any, error) {
		result := map[string]any{"sum": input.A + input.B}
		raw, _ := json.Marshal(result)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(raw)},
			},
		}, nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "fail",
		Description: "Always fails",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input struct{}) (*mcp.CallToolResult, any, error) {
		return nil, nil, fmt.Errorf("something went wrong")
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "soft_fail",
		Description: "Returns error in content",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input struct{}) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "bad input"}},
			IsError: true,
		}, nil, nil
	})

	c, err := mcptotap.New(context.Background(), server)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestList(t *testing.T) {
	c := setup(t)

	result, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(result.Tools))
	}

	names := map[string]bool{}
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}
	for _, name := range []string{"greet", "add", "fail", "soft_fail"} {
		if !names[name] {
			t.Errorf("missing tool %q", name)
		}
	}
}

func TestListToolDescriptions(t *testing.T) {
	c := setup(t)

	result, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	descs := map[string]string{}
	for _, tool := range result.Tools {
		descs[tool.Name] = tool.Description
	}

	if descs["greet"] != "Greet someone by name" {
		t.Errorf("wrong description for greet: %q", descs["greet"])
	}
}

func TestRunString(t *testing.T) {
	c := setup(t)

	raw, err := c.Run(context.Background(), "greet", map[string]any{"name": "World"})
	if err != nil {
		t.Fatal(err)
	}

	var result string
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("expected string result, got: %s", string(raw))
	}

	if result != "Hello, World!" {
		t.Errorf("expected 'Hello, World!', got %q", result)
	}
}

func TestRunStructured(t *testing.T) {
	c := setup(t)

	raw, err := c.Run(context.Background(), "add", map[string]any{"a": 3, "b": 4})
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("expected map result, got: %s", string(raw))
	}

	sum, ok := result["sum"]
	if !ok {
		t.Fatal("missing 'sum' key")
	}
	if sum.(float64) != 7 {
		t.Errorf("expected sum 7, got %v", sum)
	}
}

func TestRunStructuredWithJQ(t *testing.T) {
	c := setup(t)

	raw, err := c.Run(context.Background(), "add", map[string]any{
		"a":   3,
		"b":   4,
		"_jq": ".sum",
	})
	if err != nil {
		t.Fatal(err)
	}

	var sum float64
	if err := json.Unmarshal(raw, &sum); err != nil {
		t.Fatalf("expected numeric jq result, got: %s", string(raw))
	}
	if sum != 7 {
		t.Errorf("expected sum 7, got %v", sum)
	}
}

func TestRunJQDirectiveTypeError(t *testing.T) {
	c := setup(t)

	_, err := c.Run(context.Background(), "add", map[string]any{
		"a":   3,
		"b":   4,
		"_jq": 123,
	})
	if err == nil {
		t.Fatal("expected error for non-string _jq")
	}
	if err.Error() != "_jq must be a string" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunError(t *testing.T) {
	c := setup(t)

	_, err := c.Run(context.Background(), "fail", map[string]any{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunSoftError(t *testing.T) {
	c := setup(t)

	_, err := c.Run(context.Background(), "soft_fail", map[string]any{})
	if err == nil {
		t.Fatal("expected error for IsError result")
	}
	if err.Error() != "bad input" {
		t.Errorf("expected 'bad input', got %q", err.Error())
	}
}

func TestRunNotFound(t *testing.T) {
	c := setup(t)

	_, err := c.Run(context.Background(), "nonexistent", map[string]any{})
	if err == nil {
		t.Fatal("expected error for nonexistent tool")
	}
}

func TestGet(t *testing.T) {
	c := setup(t)

	doc, err := c.Get(context.Background(), "greet")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Name != "greet" {
		t.Fatalf("expected tool greet, got %q", doc.Name)
	}
	if doc.Description != "Greet someone by name" {
		t.Fatalf("unexpected description: %q", doc.Description)
	}
}

func TestRunStream(t *testing.T) {
	c := setup(t)

	ch, err := c.RunStream(context.Background(), "greet", map[string]any{"name": "World"})
	if err != nil {
		t.Fatal(err)
	}

	ev, ok := <-ch
	if !ok {
		t.Fatal("expected result event")
	}
	if ev.Type != "result" {
		t.Fatalf("expected result event, got %q", ev.Type)
	}
	raw, ok := ev.Data.(json.RawMessage)
	if !ok {
		t.Fatalf("expected raw json payload, got %T", ev.Data)
	}
	var msg string
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("expected string payload, got %s", raw)
	}
	if msg != "Hello, World!" {
		t.Fatalf("unexpected stream result: %q", msg)
	}

	ev, ok = <-ch
	if !ok {
		t.Fatal("expected done event")
	}
	if ev.Type != "done" {
		t.Fatalf("expected done event, got %q", ev.Type)
	}
}

func TestNewHTTPEmptyEndpoint(t *testing.T) {
	_, err := mcptotap.NewHTTP(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty endpoint")
	}
}

func TestNewRemoteAliasEmptyEndpoint(t *testing.T) {
	_, err := mcptotap.NewRemote(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty endpoint")
	}
}

func TestNewStdioNilCommand(t *testing.T) {
	_, err := mcptotap.NewStdio(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil command")
	}
}
