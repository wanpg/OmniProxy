# Codex Proxy 集成方案

> 将 Codex Responses API（ChatGPT Plus/Pro 订阅额度）集成到 gateway-proxy，对外暴露 OpenAI Chat Completions 兼容接口。

**日期**：2026-04-22
**状态**：方案设计

---

## 1. 背景与目标

### 为什么做
- ChatGPT Plus/Pro 订阅包含 Codex 额度（GPT-5、o3 等模型）
- 目前额度只能通过 Codex CLI / Codex Desktop 使用
- 通过代理转发，可以让 OpenClaw 等 OpenAI 兼容客户端直接调用，**零额外 API 费用**

### 目标
1. gateway-proxy 新增 `codex` provider 类型
2. 支持 OpenAI Chat Completions 格式输入/输出（流式 + 非流式）
3. 支持 Codex OAuth 登录 + Token 自动刷新
4. 多账号轮转 + 速率限制处理
5. 复用现有的统计、计费、Admin UI

### 不做
- ❌ 不做 Responses API 原生透传（OpenClaw 不需要）
- ❌ 不做 Anthropic / Gemini 格式输出（后续按需加）
- ❌ 不做 WebSocket 传输（HTTP SSE 够用）

---

## 2. 技术调研

### 2.1 Codex Responses API 是什么

Codex CLI/Destkop 调用的是 `https://chatgpt.com/backend-api/codex/responses`，使用的是 **OpenAI Responses API** 格式（非 Chat Completions）。

**请求格式（Responses API）**：
```json
{
  "model": "codex-mini",
  "instructions": "You are a helpful assistant.",
  "input": [
    {"role": "user", "content": "Hello"}
  ],
  "stream": true,
  "store": false
}
```

**响应格式（SSE 流式）**：
```
event: response.created
data: {"type":"response.created","response":{"id":"resp_xxx","model":"codex-mini",...}}

event: response.output_text.delta
data: {"type":"response.output_text.delta","delta":"Hello"}

event: response.completed
data: {"type":"response.completed","response":{"output":[...],"usage":{"input_tokens":10,"output_tokens":5}}}
```

### 2.2 需要的转换

**Chat Completions → Responses API（请求方向）**：

| Chat Completions | Responses API |
|---|---|
| `messages[role=system/developer]` | `instructions` |
| `messages[role=user/assistant/tool]` | `input[]` |
| `model` | `model`（可能需要映射） |
| `tools` | `tools`（格式不同，需转换） |
| `tool_calls` (assistant) | `function_call` input items |
| `tool` (role) | `function_call_output` input items |
| `stream: true` | `stream: true` |
| `response_format` | `text.format` |
| `reasoning_effort` | `reasoning.effort` |

**Responses API → Chat Completions（响应方向）**：

| Responses API SSE | Chat Completions SSE |
|---|---|
| `response.output_text.delta` → `delta.content` | `chat.completion.chunk` |
| `response.function_call_arguments.delta` | `delta.tool_calls[].function.arguments` |
| `response.completed` → `finish_reason: "stop"` | 最终 chunk + usage |
| `usage.input_tokens` | `usage.prompt_tokens` |
| `usage.output_tokens` | `usage.completion_tokens` |

### 2.3 认证机制

Codex 使用 **ChatGPT OAuth2** 认证：
1. 通过 Auth0 PKCE 流程获取 `access_token` + `refresh_token`
2. `access_token` 有效期约 1 小时，需要定期刷新
3. 请求头需要 `Authorization: Bearer <access_token>`
4. 可选 `Chatgpt-Account-Id` header

### 2.4 参考项目对比

| | codex-proxy (icebear0828) | CLIProxyAPI (router-for-me) |
|---|---|---|
| **语言** | TypeScript (Hono) | Go (Gin) |
| **核心逻辑** | ✅ 完整的 Chat↔Responses 转换 | ✅ 完整，但耦合多 provider |
| **OAuth 登录** | ✅ PKCE + Device Code | ✅ PKCE + Device Code |
| **Token 刷新** | ✅ RefreshScheduler | ✅ 内置 |
| **多账号** | ✅ AccountPool 轮转 | ✅ 多账号管理 |
| **TLS 指纹** | ✅ Rust TLS 模拟浏览器 | ✅ utls |
| **可集成性** | 独立进程 | 有 Go SDK 但依赖 Gin |
| **代码量** | ~5000 行 TS | ~20000+ 行 Go |

**结论**：两个项目都无法直接作为 Go 库嵌入（codex-proxy 是 TS，CLIProxyAPI 强依赖 Gin）。
最佳方案是**参考两个项目的核心转换逻辑，用 Go 原生实现**。转换逻辑本身不复杂（~500 行），关键代码在：
- `codex-proxy/src/translation/openai-to-codex.ts`（请求转换）
- `codex-proxy/src/translation/codex-to-openai.ts`（响应转换）

---

## 3. 架构设计

### 3.1 整体架构

```
OpenClaw / 外部客户端
    │  POST /v1/chat/completions (OpenAI 格式)
    ▼
gateway-proxy (Go)
    │  1. 认证（现有 key 机制）
    │  2. 模型路由（codex provider）
    │  3. Chat Completions → Responses API 转换
    │  4. 请求转发（带 Codex OAuth token）
    ▼
chatgpt.com/backend-api/codex/responses
    │  SSE 流式响应
    ▼
gateway-proxy
    │  5. Responses API → Chat Completions 转换
    │  6. 统计日志（SQLite）
    ▼
客户端
```

### 3.2 新增文件结构

```
gateway-proxy/
├── main.go                    # 新增 codex 路由注册
├── config.go                  # 新增 codex provider 配置
├── handlers.go                # 新增 auth_type: "codex" 分支
├── codex.go                   # 🆕 Codex 核心逻辑（转换+转发）
├── codex_auth.go              # 🆕 OAuth 登录 + Token 刷新
├── codex_translate.go         # 🆕 请求/响应格式转换
├── codex_pool.go              # 🆕 多账号池管理
├── codex_types.go             # 🆕 Codex 相关类型定义
├── config.yaml                # 新增 codex provider 配置段
└── docs/
    └── CODEX_INTEGRATION_DESIGN.md
```

### 3.3 配置格式扩展

```yaml
providers:
  # ... 现有 provider 不变 ...

  codex:
    api_base: "https://chatgpt.com/backend-api/codex"  # 可选，有默认值
    models:
      - "gpt-5.4"
      - "o3"
      - "codex-mini"
    auth_type: "codex"  # 🆕 新增类型
    # 多账号管理
    accounts:
      - access_token: ""       # 运行时自动填充
        refresh_token: "xxx"
        account_id: "yyy"
        status: "active"
    # 速率限制
    rate_limit_backoff: 60     # 触发限流后等待秒数
    # 轮转策略
    rotation: "least_used"     # least_used | round_robin

keys:
  # ... 现有 keys 不变 ...
  - key: "sk-codex-proxy-xxxxx"
    alias: "Codex-User"
    provider: ""
    models: []
```

---

## 4. 核心模块设计

### 4.1 请求转换（Chat → Responses）

```go
// codex_translate.go

// ChatToResponses 将 OpenAI Chat Completions 请求转换为 Codex Responses API 请求
func ChatToResponses(chatReq *ChatCompletionRequest) (*CodexResponsesRequest, error) {
    req := &CodexResponsesRequest{
        Model:        resolveModel(chatReq.Model),
        Instructions: extractInstructions(chatReq.Messages),
        Input:        convertMessagesToInput(chatReq.Messages),
        Stream:       chatReq.Stream,
        Store:        false,
    }

    // 工具转换
    if len(chatReq.Tools) > 0 {
        req.Tools = convertTools(chatReq.Tools)
    }

    // Reasoning effort
    if chatReq.ReasoningEffort != "" {
        req.Reasoning = &Reasoning{Effort: chatReq.ReasoningEffort, Summary: "auto"}
    }

    // Response format → text.format
    if chatReq.ResponseFormat != nil {
        req.Text = convertResponseFormat(chatReq.ResponseFormat)
    }

    return req, nil
}
```

**消息转换规则**：
- `system` / `developer` → 合并为 `instructions`
- `user` → `input: [{role: "user", content: "..."}]`
- `assistant` (纯文本) → `input: [{role: "assistant", content: "..."}]`
- `assistant` + `tool_calls` → assistant text item + 多个 `function_call` items
- `tool` → `function_call_output` items

### 4.2 响应转换（Responses → Chat）

```go
// codex_translate.go

// StreamResponsesToChat 将 Codex SSE 流转换为 OpenAI Chat Completions SSE 流
func StreamResponsesToChat(
    src io.Reader,           // 上游 SSE 流
    dst http.ResponseWriter,  // 下游 ResponseWriter（flush 支持）
    model string,
) (usage *TokenUsage, err error) {
    // 1. 解析 SSE 事件
    // 2. response.output_text.delta → 写 chat.completion.chunk (delta.content)
    // 3. response.function_call_arguments.delta → 写 chat.completion.chunk (delta.tool_calls)
    // 4. response.completed → 提取 usage，写最终 chunk + [DONE]
}
```

### 4.3 OAuth 认证 & Token 管理

```go
// codex_auth.go

type CodexAuth struct {
    // OAuth 配置（ChatGPT Auth0）
    AuthDomain    string  // "auth.openai.com"
    ClientID      string  // "pdlLIX2Y72MIl2rhLhTE9VV9bN905kBh"
    RedirectURL   string  // "http://localhost:{port}/auth/callback"
    AccountPool   *AccountPool
    RefreshTicker *time.Ticker
}

// StartOAuthFlow 启动 PKCE OAuth 流程，返回授权 URL
func (a *CodexAuth) StartOAuthFlow() (authURL string, state string, err error)

// HandleCallback 处理 OAuth 回调，交换 token
func (a *CodexAuth) HandleCallback(code, state string) error

// RefreshToken 刷新 access_token
func (a *CodexAuth) RefreshToken(refreshToken string) (*TokenPair, error)

// AutoRefresh 定期刷新所有账号的 token
func (a *CodexAuth) AutoRefresh(ctx context.Context)
```

### 4.4 多账号池

```go
// codex_pool.go

type AccountEntry struct {
    ID           string    `json:"id"`
    AccessToken  string    `json:"access_token"`
    RefreshToken string    `json:"refresh_token"`
    AccountID    string    `json:"account_id"`
    Email        string    `json:"email"`
    Status       string    `json:"status"`  // active | expired | rate_limited
    UsageCount   int       `json:"usage_count"`
    LastUsed     time.Time `json:"last_used"`
    RateLimitReset time.Time `json:"rate_limit_reset,omitempty"`
}

type AccountPool struct {
    mu       sync.RWMutex
    accounts []AccountEntry
    rotation string  // "least_used" | "round_robin"
    index    int     // round_robin 用
}

// Acquire 获取一个可用账号
func (p *AccountPool) Acquire() (*AccountEntry, error)

// Release 归还账号（更新使用计数、处理限流）
func (p *AccountPool) Release(entryID string, rateLimited bool, resetAt *time.Time)

// MarkExpired 标记账号过期
func (p *AccountPool) MarkExpired(entryID string)

// Persist 持久化到文件
func (p *AccountPool) Persist(path string) error
```

### 4.5 handlers.go 改动

```go
// proxyRequest 中新增 codex 分支

func proxyRequest(w http.ResponseWriter, r *http.Request, ...) {
    provider := globalConfig.GetProvider(providerName)

    switch provider.AuthType {
    case "openai":
        // 现有逻辑不变
    case "anthropic":
        // 现有逻辑不变
    case "codex":
        // 🆕 Codex 处理
        handleCodexProxy(w, r, provider, alias, hash, model, body)
        return
    }
}
```

---

## 5. 实现计划

### Phase 1：核心转发（最小可用）
**目标**：手动配置 token，能跑通 Chat Completions → Codex → Chat Completions

| 任务 | 文件 | 预估 |
|------|------|------|
| Codex 类型定义 | `codex_types.go` | 0.5h |
| 请求转换 Chat→Responses | `codex_translate.go` | 1.5h |
| 响应转换 Responses→Chat（流式） | `codex_translate.go` | 2h |
| Codex 代理 handler | `codex.go` | 1.5h |
| config.go 扩展（auth_type: codex） | `config.go` | 0.5h |
| handlers.go 分支 | `handlers.go` | 0.5h |
| main.go 路由注册 | `main.go` | 0.5h |
| config.yaml 示例 | `config.yaml` | 0.5h |
| **合计** | | **7.5h** |

### Phase 2：OAuth + 多账号
**目标**：Web 登录、自动刷新、多账号轮转

| 任务 | 文件 | 预估 |
|------|------|------|
| OAuth PKCE 流程 | `codex_auth.go` | 2h |
| Token 刷新 | `codex_auth.go` | 1h |
| 多账号池 | `codex_pool.go` | 1.5h |
| 账号持久化 | `codex_pool.go` | 0.5h |
| 登录/管理 API | `codex.go` | 1h |
| 速率限制处理 | `codex.go` | 1h |
| **合计** | | **7h** |

### Phase 3：打磨
**目标**：Admin UI 集成、TLS 指纹（如需）、错误处理

| 任务 | 预估 |
|------|------|
| Admin UI 新增 Codex 账号管理面板 | 2h |
| 模型映射（用户传 gpt-5.4 → codex-mini） | 0.5h |
| TLS 指纹（curl-impersonate 或 utls） | 2h（可能不需要） |
| 错误码映射 + 重试逻辑 | 1h |
| **合计** | **5.5h** |

---

## 6. 关键风险

| 风险 | 影响 | 缓解 |
|------|------|------|
| **TLS 指纹检测** | ChatGPT 可能检测非浏览器 TLS 指纹并拒绝 | Phase 1 先用标准 HTTP 试；如被拒绝，Phase 3 加 utls 模拟 |
| **Token 刷新失败** | 账号掉线 | 重试 + 通知 + 手动重新登录 |
| **Codex API 变更** | 格式转换失效 | Responses API 已稳定；关注 OpenAI changelog |
| **速率限制严格** | Plus 用户 5h/7d 额度 | 多账号轮转 + 速率限制退避 |
| **CGO 编译** | 已有踩坑经验 | 复用现有 Dockerfile 方案 |

---

## 7. 测试计划

1. **单元测试**：请求/响应转换（Chat ↔ Responses）
2. **集成测试**：
   - 手动 token → 发送 Chat Completions → 验证响应格式
   - 流式 SSE 正确输出
   - Token 使用量统计正确
3. **E2E 测试**：
   - OpenClaw → gateway-proxy → Codex → 返回结果
   - 多轮对话 + tool calling

---

## 8. 上线清单

- [ ] Phase 1 代码完成 + 单元测试
- [ ] Docker 构建通过
- [ ] 手动 token 配置测试
- [ ] OpenClaw 配置 codex provider
- [ ] Agent 模型切换测试
- [ ] Phase 2 OAuth 登录
- [ ] 多账号 + Token 自动刷新
- [ ] Admin UI 账号管理
- [ ] 监控告警配置
