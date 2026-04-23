# OmniProxy

A lightweight multi-provider LLM proxy written in Go. Unified OpenAI-compatible API for multiple providers.

## Features

- **Multi-provider support** — OpenAI, Anthropic, MiniMax, Zhipu (GLM), and [Codex Responses API](docs/CODEX_INTEGRATION_DESIGN.md) (ChatGPT Plus/Pro subscription)
- **OpenAI-compatible output** — All providers exposed as standard Chat Completions API (`/v1/chat/completions`)
- **Streaming** — Full SSE streaming support with real-time flush
- **API key routing** — Route different keys to different providers/models
- **Multi-account pool** — Load balancing across multiple accounts (for Codex)
- **Auto token refresh** — Reads `~/.codex/auth.json` automatically, refreshes on 401
- **Usage tracking** — Per-request logging with SQLite storage
- **Admin dashboard** — Built-in stats API
- **Single binary** — No runtime dependencies, just run it

## Quick Start

### Build

```bash
# Standard build
go build -o omniproxy .

# CGO build (for SQLite)
CGO_ENABLED=1 go build -o omniproxy .
```

### Docker

```bash
docker build -t omniproxy .
docker run -d -p 8080:8080 \
  -v $(pwd)/config.yaml:/config.yaml \
  omniproxy
```

### Configure

```bash
cp config.yaml.example config.yaml
# Edit config.yaml with your provider keys
```

### Run

```bash
./omniproxy
```

The proxy starts on `:8080` and exposes:

| Endpoint | Description |
|----------|-------------|
| `GET /v1/models` | List available models |
| `POST /v1/chat/completions` | Chat Completions API (OpenAI-compatible) |
| `GET /health` | Health check |
| `GET /admin/stats` | Usage statistics |

## Configuration

See [config.yaml.example](config.yaml.example) for full configuration options.

### Provider Setup

#### Standard providers (API key)

```yaml
providers:
  openai:
    api_base: "https://api.openai.com/v1"
    api_key: "sk-your-key"
    models:
      - "gpt-4o"
    auth_type: "openai"
```

#### Codex (ChatGPT Plus/Pro)

No API key needed — automatically reads from `~/.codex/auth.json` (created by `codex auth login`).

```yaml
providers:
  codex:
    api_base: "https://chatgpt.com/backend-api/codex"
    models:
      - "gpt-5.4"
    auth_type: "codex"
```

Or configure manually:

```yaml
  codex:
    auth_type: "codex"
    accounts:
      - access_token: "your-token"
        account_id: "optional"
```

### Key Routing

Route specific API keys to specific providers:

```yaml
keys:
  - key: "sk-my-key"
    provider: "openai"
    models: ["gpt-4o"]  # Empty = all models from provider
```

## Codex Integration

OmniProxy supports the [Codex Responses API](docs/CODEX_INTEGRATION_DESIGN.md), allowing you to use your ChatGPT Plus/Pro subscription as a model provider. This converts between OpenAI Chat Completions format and Codex Responses format transparently.

Supported models: `gpt-5.4` (more models may be added as OpenAI enables them).

## Architecture

```
Client (OpenAI SDK, curl, etc.)
    │
    ▼
┌─────────────┐
│  OmniProxy   │  ← Single binary, :8080
│              │
│  /v1/chat/   │
│  completions │
│      │       │
│  ┌───┴───┐   │
│  │Router │   │  ← Key → Provider mapping
│  └───┬───┘   │
│      │       │
│  ┌───┴──────────────────┐
│  │                      │
│  ▼         ▼            ▼          ▼
│ OpenAI   Anthropic   MiniMax   Zhipu   Codex
│  API      API         API       API    Responses API
└─────────────────────────────────────────────┘
```

## Tech Stack

- **Go** — net/http standard library (no frameworks)
- **SQLite** — Usage tracking (CGO)
- **YAML** — Configuration
- **Docker** — Deployment

## License

[MIT](LICENSE)
