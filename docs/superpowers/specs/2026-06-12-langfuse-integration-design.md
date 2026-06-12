# Langfuse 集成设计文档

## 目标

在 API 使用时将请求元数据记录到 Langfuse 可观测性平台，支持按 API Key (Token) 粒度配置各自的 Langfuse 项目。

## 需求

- **配置层级**：API Key (Token) 级别，每个 Token 可独立配置 Langfuse
- **记录内容**：基本元数据（模型名、token 用量、耗时、费用）+ 错误状态信息 + 用户信息。**不记录**输入/输出内容
- **集成方式**：使用官方 Go SDK `github.com/git-hulk/langfuse-go`
- **记录时机**：请求完成后一次性生成 trace 并发送

## 数据模型

### Token 表新增字段

在 `model/token.go` 的 `Token` struct 中添加三个可选字段：

| 字段 | Go 类型 | DB 列 | 说明 |
|------|---------|--------|------|
| `LangfusePublicKey` | `string` | `langfuse_public_key TEXT` | Langfuse Public Key |
| `LangfuseSecretKey` | `string` | `langfuse_secret_key TEXT` | Langfuse Secret Key（加密存储） |
| `LangfuseHost` | `string` | `langfuse_host TEXT` | Langfuse Host，默认 `https://cloud.langfuse.com` |

三个字段全部非空时才启用 Langfuse 追踪。

### SecretKey 安全

- 数据库中加密存储（使用项目现有的加密工具 `common/crypto.go`）
- API 响应中脱敏显示（仅返回最后 4 位，其余用 `*` 替代）
- 编辑时，如果传入的值与脱敏格式匹配，则不更新该字段

### 数据库迁移

在 `model/main.go` 的 `createTables` 中通过 GORM AutoMigrate 自动添加列，兼容 SQLite/MySQL/PostgreSQL。

## 架构设计

### 模块结构

```
common/langfuse.go           — Langfuse 客户端池管理
controller/relay.go          — 在请求完成后调用 Langfuse 记录（修改）
model/token.go               — Token 模型添加字段（修改）
middleware/auth.go            — TokenAuth 中将 Langfuse 配置写入 context（修改）
dto/token.go                 — Token DTO 添加 Langfuse 字段（修改）
controller/token.go          — Token 创建/更新 API 处理脱敏（修改）
```

### Langfuse 客户端池 (`common/langfuse.go`)

以 `(publicKey, secretKey, host)` 三元组为 key 缓存 `langfuse.Client` 实例：

```go
type LangfuseManager struct {
    mu      sync.RWMutex
    clients map[string]*langfuse.Client  // key = sha256(publicKey+secretKey+host)
}

func GetLangfuseManager() *LangfuseManager
func (m *LangfuseManager) GetClient(publicKey, secretKey, host string) (*langfuse.Client, error)
func (m *LangfuseManager) RecordTrace(ctx context.Context, config LangfuseConfig, traceData LangfuseTraceData)
func (m *LangfuseManager) Close()
```

- 客户端创建后缓存复用，避免每次请求都创建
- SecretKey 在 key 计算前做哈希，不存明文 key

### Context 传递

在 `middleware/auth.go` 的 `SetupContextForToken()` 中，将 Token 的 Langfuse 配置写入 Gin context：

```go
c.Set("langfuse_public_key", token.LangfusePublicKey)
c.Set("langfuse_secret_key", token.LangfuseSecretKey)
c.Set("langfuse_host", token.LangfuseHost)
```

### Trace 记录 (`common/langfuse.go`)

`RecordTrace` 方法在请求完成后调用：

```go
type LangfuseConfig struct {
    PublicKey string
    SecretKey string
    Host      string
}

type LangfuseTraceData struct {
    RequestID        string
    UserID           int
    TokenName        string
    ModelName        string
    ChannelID        int
    Group            string
    IsStream         bool
    PromptTokens     int
    CompletionTokens int
    TotalTokens      int
    UseTime          int64         // 毫秒
    Quota            int64
    QuotaUnit        string
    Success          bool
    StatusCode       int
    ErrorMessage     string
}
```

记录逻辑：
1. 检查三个配置字段是否都非空，否则跳过
2. 从客户端池获取/创建 Langfuse Client
3. 创建 Trace（name = model name）
   - `Trace.UserID` = 用户 ID
   - `Trace.Metadata` = { token_name, channel_id, group, request_id }
   - `Trace.Tags` = [model_name, stream/non-stream, success/error]
4. 创建 Generation（name = "completion"）
   - `Generation.Model` = model name
   - `Generation.Usage` = { Input, Output, Total }
   - `Generation.Metadata` = { latency_ms, quota_cost }
5. 如果失败，记录 `StatusMessage` 为错误信息
6. `Generation.End()` → `Trace.End()` → `Client.Flush()`

### 调用位置

在 `controller/relay.go` 中：
- **成功路径**：`PostConsumeQuota()` 之后，调用 `RecordTrace`
- **失败路径**：错误处理（`RelayErrorHandler`）中，调用 `RecordTrace`

使用 `gopool` 异步执行，不阻塞请求响应。

## 前端变更

### Token 创建/编辑表单

在表单中添加可折叠的 "Langfuse 可观测性配置" 区域：
- Langfuse Host（输入框，placeholder: `https://cloud.langfuse.com`）
- Public Key（输入框）
- Secret Key（密码输入框，编辑时显示脱敏值）

### 涉及文件

- Token 编辑表单组件：添加 Langfuse 配置折叠区
- Token 类型定义：添加 `langfuse_public_key`, `langfuse_secret_key`, `langfuse_host` 字段
- i18n 翻译文件：添加相关翻译 key

## 错误处理

- Langfuse 记录失败时仅打印 warning 日志，不影响 API 请求的正常响应
- 客户端池创建失败时跳过记录
- 异步执行确保不阻塞主流程

## 不做的事情

- 不记录输入/输出内容（用户未选择）
- 不做全局级别 Langfuse 配置（仅 Token 级别）
- 不在请求过程中实时记录（仅请求完成后）
- 不做 OpenTelemetry 集成
