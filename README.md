# mcptotap

`mcptotap` is an adapter that lets MCP servers be used through Tool API Pattern (TAP) clients.

See [`tap-go`](https://github.com/mhpenta/tap-go) for protocol details and reference TAP tooling.

## Why this exists

This adapter bridges an MCP server into a `tap-go/client.ToolClient` so TAP-oriented agent/tooling code can call MCP tools without rewriting business logic.

## What it provides

- In-process MCP adapter (`New`)
- Remote HTTP MCP adapter (`NewHTTP`, `NewRemote` alias)
- Stdio MCP adapter (`NewStdio`)
- `List`, `Get`, `Run`, and `RunStream` methods compatible with TAP client expectations

## Usage

```go
// HTTP MCP endpoint
c, err := mcptotap.NewHTTP(
    context.Background(),
    "https://www.example.com/mcp",
    mcptotap.WithBearerToken(os.Getenv("EXAMPLE_API_KEY")),
)
if err != nil {
    return nil, fmt.Errorf("unable to create MCP adapter: %w", err)
}

// Stdio MCP command
cmd := exec.Command(
    "/Users/me/bin/my-mcp-server",
    "--mcp",
    "--mcp-transport",
    "stdio",
)
cmd.Dir = "/Users/me/projects/my-mcp-server"

c, err = mcptotap.NewStdio(context.Background(), cmd)
if err != nil {
    return nil, fmt.Errorf("unable to create stdio MCP adapter: %w", err)
}
```
