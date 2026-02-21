package mcptotap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"github.com/mhpenta/tap-go"
	"github.com/mhpenta/tap-go/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Client struct {
	session *mcp.ClientSession
}

var _ client.ToolClient = (*Client)(nil)

func New(ctx context.Context, server *mcp.Server) (*Client, error) {
	ct, st := mcp.NewInMemoryTransports()

	_, err := server.Connect(ctx, st, nil)
	if err != nil {
		return nil, fmt.Errorf("mcptotap: server connect: %w", err)
	}

	c := mcp.NewClient(&mcp.Implementation{
		Name:    "mcptotap",
		Version: "0.1.0",
	}, nil)

	session, err := c.Connect(ctx, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("mcptotap: client connect: %w", err)
	}

	return &Client{session: session}, nil
}

func NewStdio(ctx context.Context, cmd *exec.Cmd) (*Client, error) {
	if cmd == nil {
		return nil, fmt.Errorf("mcptotap: nil command")
	}

	c := mcp.NewClient(&mcp.Implementation{
		Name:    "mcptotap",
		Version: "0.1.0",
	}, nil)

	session, err := c.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		return nil, fmt.Errorf("mcptotap: stdio connect: %w", err)
	}

	return &Client{session: session}, nil
}

type RemoteOption func(*remoteConfig)

type remoteConfig struct {
	bearerToken string
	xAPIKey     string
	httpClient  *http.Client
	useSSE      bool
}

func WithBearerToken(token string) RemoteOption {
	return func(cfg *remoteConfig) { cfg.bearerToken = token }
}

func WithXAPIKey(key string) RemoteOption {
	return func(cfg *remoteConfig) { cfg.xAPIKey = key }
}

func WithHTTPClient(c *http.Client) RemoteOption {
	return func(cfg *remoteConfig) { cfg.httpClient = c }
}

func WithSSE() RemoteOption {
	return func(cfg *remoteConfig) { cfg.useSSE = true }
}

func NewHTTP(ctx context.Context, endpoint string, opts ...RemoteOption) (*Client, error) {
	var cfg remoteConfig
	for _, o := range opts {
		o(&cfg)
	}

	if strings.TrimSpace(endpoint) == "" {
		return nil, fmt.Errorf("mcptotap: empty endpoint")
	}
	httpClient := buildHTTPClient(cfg)

	var transport mcp.Transport
	if cfg.useSSE {
		transport = &mcp.SSEClientTransport{
			Endpoint:   endpoint,
			HTTPClient: httpClient,
		}
	} else {
		transport = &mcp.StreamableClientTransport{
			Endpoint:   endpoint,
			HTTPClient: httpClient,
		}
	}

	c := mcp.NewClient(&mcp.Implementation{
		Name:    "mcptotap",
		Version: "0.1.0",
	}, nil)

	session, err := c.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("mcptotap: remote connect %s: %w", endpoint, err)
	}

	return &Client{session: session}, nil
}

func NewRemote(ctx context.Context, endpoint string, opts ...RemoteOption) (*Client, error) {
	return NewHTTP(ctx, endpoint, opts...)
}

type headerTransport struct {
	key   string
	value string
	base  http.RoundTripper
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set(t.key, t.value)
	return t.base.RoundTrip(req)
}

func buildHTTPClient(cfg remoteConfig) *http.Client {
	var hc http.Client
	if cfg.httpClient != nil {
		hc = *cfg.httpClient
	} else {
		hc = http.Client{}
	}
	base := hc.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	if cfg.bearerToken != "" {
		base = &headerTransport{
			key:   "Authorization",
			value: "Bearer " + cfg.bearerToken,
			base:  base,
		}
	}
	if cfg.xAPIKey != "" {
		base = &headerTransport{
			key:   "x-api-key",
			value: cfg.xAPIKey,
			base:  base,
		}
	}
	hc.Transport = base
	return &hc
}

func (c *Client) Close() error {
	return c.session.Close()
}

func (c *Client) List(ctx context.Context) (*client.ListResult, error) {
	result, err := c.session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return nil, err
	}

	out := &client.ListResult{}
	for _, t := range result.Tools {
		out.Tools = append(out.Tools, client.Tool{
			Name:        t.Name,
			Description: t.Description,
		})
	}
	return out, nil
}

func (c *Client) Get(ctx context.Context, name string) (*client.ToolDoc, error) {
	result, err := c.session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return nil, err
	}

	for _, t := range result.Tools {
		if t.Name != name {
			continue
		}
		doc := &client.ToolDoc{
			Name:        t.Name,
			Description: t.Description,
		}
		if params, ok := t.InputSchema.(map[string]any); ok {
			doc.Parameters = params
		}
		return doc, nil
	}

	return nil, tap.NewError(tap.ErrNotFound, "tool not found: "+name)
}

func (c *Client) Run(ctx context.Context, name string, args any) (json.RawMessage, error) {
	serverArgs, directives, err := splitRunArgs(args)
	if err != nil {
		return nil, err
	}

	result, err := c.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      name,
		Arguments: serverArgs,
	})
	if err != nil {
		return nil, err
	}

	if result.IsError {
		return nil, fmt.Errorf("%s", extractText(result))
	}

	var raw json.RawMessage
	if result.StructuredContent != nil {
		raw, err = json.Marshal(result.StructuredContent)
		if err != nil {
			return nil, err
		}
	} else {
		raw, err = json.Marshal(extractResult(result))
		if err != nil {
			return nil, err
		}
	}

	if directives.JQ == "" {
		return raw, nil
	}

	filtered, err := client.JQ(raw, directives.JQ)
	if err != nil {
		return nil, fmt.Errorf("run tool %s jq filter: %w", name, err)
	}
	return filtered, nil
}

func (c *Client) RunStream(ctx context.Context, name string, args any) (<-chan tap.Event, error) {
	raw, err := c.Run(ctx, name, args)
	if err != nil {
		return nil, err
	}

	ch := make(chan tap.Event, 2)
	ch <- tap.Event{Type: "result", Data: raw}
	ch <- tap.Event{Type: "done"}
	close(ch)
	return ch, nil
}

type runDirectives struct {
	JQ string
}

func splitRunArgs(args any) (any, runDirectives, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, runDirectives{}, err
	}

	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return args, runDirectives{}, nil
	}

	directives := runDirectives{}
	hasDirective := false

	if v, ok := obj["_jq"]; ok {
		hasDirective = true
		s, ok := v.(string)
		if !ok {
			return nil, runDirectives{}, fmt.Errorf("_jq must be a string")
		}
		directives.JQ = s
		delete(obj, "_jq")
	}

	if !hasDirective {
		return args, directives, nil
	}
	return obj, directives, nil
}

func extractText(result *mcp.CallToolResult) string {
	var parts []string
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	if len(parts) == 0 {
		return "tool returned an error"
	}
	return strings.Join(parts, "\n")
}

func extractResult(result *mcp.CallToolResult) any {
	if len(result.Content) == 1 {
		if tc, ok := result.Content[0].(*mcp.TextContent); ok {
			var v any
			if err := json.Unmarshal([]byte(tc.Text), &v); err == nil {
				return v
			}
			return tc.Text
		}
	}

	if len(result.Content) > 0 {
		var parts []any
		for _, c := range result.Content {
			switch v := c.(type) {
			case *mcp.TextContent:
				var parsed any
				if err := json.Unmarshal([]byte(v.Text), &parsed); err == nil {
					parts = append(parts, parsed)
				} else {
					parts = append(parts, v.Text)
				}
			default:
				raw, _ := json.Marshal(c)
				var parsed any
				json.Unmarshal(raw, &parsed)
				parts = append(parts, parsed)
			}
		}
		return parts
	}

	return nil
}
