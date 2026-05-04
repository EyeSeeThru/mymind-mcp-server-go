# MyMind MCP Server (Go)

A [Model Context Protocol (MCP)](https://modelcontextprotocol.io) server for the [MyMind API](https://api.mymind.com), written in Go. Compiles to a single portable binary — no runtime required.

## Features

- **Objects** — list, get, create, update, delete, download
- **Search** — keyword and semantic search
- **Tags** — add, remove, list
- **Spaces** — list, create, get, delete
- **Related** — find semantically related objects

## Why Go?

This is a port of the [Python version](https://github.com/your-username/mymind-mcp-server) with one key difference: it compiles to a standalone binary. No Python, no dependencies, no installation needed on the target machine. Drop the binary and it runs.

## Installation

### Pre-built binary

Download from the [Releases](https://github.com/your-username/mymind-mcp-server-go/releases) page for your OS/architecture.

### Build from source

```bash
git clone https://github.com/your-username/mymind-mcp-server-go.git
cd mymind-mcp-server-go
go build -o mymind-mcp .
```

### Install via go install

```bash
go install github.com/your-username/mymind-mcp-server-go@latest
```

## Configuration

The server reads the access key from the first available source:

1. `MYMIND_ACCESS_KEY` environment variable
2. `~/.mymind_mcp_access_key` file (plain text, single line)
3. `~/.mymind_mcp_config.yaml` YAML config file

### Config file formats

**Plain text** (`~/.mymind_mcp_access_key`):
```
your_kid.secret_key_here
```

**YAML** (`~/.mymind_mcp_config.yaml`):
```yaml
access_key: "your_kid.secret_key_here"
# base_url: "https://api.mymind.com"  # optional, for custom deployments
```

Get your access key from your MyMind account (`kid.secret` format).

## Usage

```bash
# With env var
MYMIND_ACCESS_KEY='your_key' ./mymind-mcp

# With default key file
./mymind-mcp

# With custom config
./mymind-mcp --config /path/to/config.yaml
```

## MCP Client Configuration

#### Claude Desktop (macOS/Windows)

Add to `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "mymind": {
      "command": "/path/to/mymind-mcp"
    }
  }
}
```

#### Cursor

Add to Cursor settings (JSON):

```json
{
  "mcpServers": {
    "mymind": {
      "command": "/path/to/mymind-mcp"
    }
  }
}
```

#### Claude Code

```bash
claude --acp -- /path/to/mymind-mcp
```

## Available Tools

| Tool | Description |
|------|-------------|
| `list_objects` | List objects. Optional: `q` (search), `limit` |
| `get_object` | Get single object by ID. Optional: `contentAs` |
| `create_object` | Create object. Params: `title`, `content`, `url`, `tags[]`, `spaces[]` |
| `update_object` | Update object title by ID |
| `delete_object` | Delete object by ID |
| `download_object` | Download object content (returns base64) |
| `search` | Search objects by keyword. Params: `query`, `limit` |
| `add_tags` | Add tags to object |
| `remove_tag` | Remove tag from object |
| `list_spaces` | List all spaces |
| `create_space` | Create space by name |
| `get_space` | Get space by ID |
| `delete_space` | Delete space by ID |
| `list_tags` | List all tags |
| `related` | Find related objects by ID |

## Cross-compilation

Build for multiple platforms from macOS:

```bash
# Linux amd64
GOOS=linux GOARCH=amd64 go build -o mymind-mcp-linux-amd64 .

# Linux arm64
GOOS=linux GOARCH=arm64 go build -o mymind-mcp-linux-arm64 .

# Windows
GOOS=windows GOARCH=amd64 go build -o mymind-mcp-win-amd64.exe .

# macOS arm64 (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o mymind-mcp-darwin-arm64 .
```

## Architecture

The server implements the MCP stdio transport:

1. Server starts, sends `initialize` response with capabilities
2. Client sends `tools/list` → server returns tool manifest
3. Client sends `tools/call` → server calls MyMind API, returns JSON result
4. Repeat until stdin closes

The MyMind API uses HS256-signed JWTs for authentication. Each request is signed with the access key, bound to the request path and HTTP method.

## License

MIT
