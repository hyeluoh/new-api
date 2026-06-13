# Langfuse 集成设计文档

## 目标

在 API 使用时将请求元数据记录到 Langfuse 可观测性平台，支持按 API Key (Token) 粒度配置各自的 Langfuse 项目。

## 需求

- **配置层级**：API Key (Token) 级别，每个 Token 可独立配置 Langfuse
- **记录内容**：基本元数据（模型名、token 用量、耗时、费用）+ 错误状态信息 + 用户信息。**不记录**输入/输出内容
- **集成方式**：使用 Go SDK `github.com/git-hulk/langfuse-go`（当前 v0.1.0）
- **记录时机**：请求完成后一次性生成 trace 并发送

## 数据模型

### Token 表新增字段

在 `model/token.go` 的 `Token` struct 中添加三个可选字段：

| 字段 | Go 类型 | DB 列 | 说明 |
|------|---------|--------|------|
| `LangfusePublicKey` | `string` | `langfuse_public_key TEXT` | Langfuse Public Key |
| `LangfuseSecretKey` | `string` | `langfuse_secret_key TEXT` | Langfuse Secret Key |
| `LangfuseHost` | `string` | `langfuse_host TEXT` | Langfuse Host，默认 `https://cloud.langfuse.com` |

三个字段全部非空时才启用 Langfuse 追踪。

### SecretKey 安全

> **重要修正**：项目现有的 `common/crypto.go` 仅提供 HMAC / bcrypt 口令哈希，**没有任何对称加解密能力**（无 AES/GCM/Encrypt/Decrypt）。因此无法按"加密存储"实现。改为沿用项目里 `Token.Key` 的既有模式：

- **明文存储**到数据库（与 `token.Key` 完全一致）
- API 响应中**脱敏显示**：复用现有 `buildMaskedTokenResponse` 脱敏通道，仅返回最后 4 位，其余用 `*` 替代
- **编辑更新策略**：前端编辑表单**不回填** SecretKey（输入框留空 + placeholder 提示"留空保持不变"）；后端收到**空值则不更新**该字段，避免把脱敏占位值回写覆盖真实密钥

> 与 `Token.Key`（明文存库 + `GetMaskedKey` 脱敏返回）的处理方式保持一致，优先一致性而非另引入一套加密体系。若未来确需静态加密存储，应单独引入 AES-GCM 密钥管理，不在本次范围内。

### 数据库迁移

新增字段通过 GORM `AutoMigrate` 自动加列（`model/main.go` 的 `migrateDB` / `migrateDBFast` 已覆盖 Token 表），兼容 SQLite/MySQL/PostgreSQL，无需手写迁移代码。

## 架构设计

### 模块结构

```
common/langfuse.go           — Langfuse 客户端池管理（新建）
common/langfuse_helper.go    — 从 gin context 提取配置并触发记录（新建）
model/token.go               — Token 模型添加字段（修改）
middleware/auth.go           — TokenAuth 中将 Langfuse 配置写入 context（修改）
controller/token.go          — 创建/更新处理 + 脱敏通道（修改）
controller/relay.go          — 失败路径记录（修改）
service/text_quota.go        — 文本补全成功后记录（修改）
service/quota.go             — Audio / WSS 成功后记录（修改）
```

### Langfuse 客户端池 (`common/langfuse.go`)

以 `(publicKey, secretKey, host)` 三元组的哈希为 key 缓存 SDK 客户端实例（SDK 客户端类型为 `*langfuse.Langfuse`）：

```go
type langfuseManager struct {
    mu      sync.RWMutex
    clients map[string]*langfuse.Langfuse  // key = sha256(publicKey+secretKey+host)
}
```

要点：

- **客户端创建**：`client := langfuse.NewClient(host, publicKey, secretKey)`（**该函数不返回 error**）。创建后缓存复用。
- **缓存 key**：对三元组做 sha256，不存明文 secret。
- **不主动淘汰**：Token 的 Langfuse 配置通常长期不变，池规模 ≈ 启用 Langfuse 的 Token 数，量级有限，暂不做 LRU。配置删除/变更后旧 client（含后台 goroutine）会驻留；可接受的取舍。如未来需要，可在 Token 更新时主动 Close 旧 client。
- **进程退出**：SDK 内部有异步批量管线，**必须在退出时调用 `client.Close()`** 才能刷出队列尾部数据。`CloseLangfuse()` 遍历所有缓存 client 调用 `Close()`。

### Context 传递

在 `middleware/auth.go` 的 `SetupContextForToken()` 中，将 Token 的 Langfuse 配置写入 Gin context（沿用项目已有的 `c.Set` / context key 机制）。

### Trace 记录

`RecordTrace` 内部使用 `gopool` 异步执行（**调用方无需再包一层 `gopool.Go`**），逻辑：

1. 检查三个配置字段是否都非空，否则跳过
2. 从客户端池获取/创建 Langfuse 客户端
3. 创建 Trace（name = model name）
   - `Trace.UserID` = 用户 ID（string）
   - `Trace.Metadata` = { token_name, channel_id, group, request_id }
   - `Trace.Tags` = [model_name, stream/non-stream, success/error]
4. 创建 Generation：`trace.StartGeneration("completion")` 返回 `*traces.Observation`
   - `Observation.Model` = model name
   - `Observation.Usage` = { Input, Output, Total, Unit: traces.UnitTokens }
   - `Observation.Metadata` = { latency_ms, quota_cost }
5. 失败时设置 `Observation.StatusMessage` 为错误信息
6. `Observation.End()` → `Trace.End()`

> **不要在每个 trace 结束后调用 `client.Flush()`**。SDK 内部已按"100 条/批 或 3 秒"自动批量发送，每请求 Flush 会退化为"每条 trace 一次 HTTP POST"，且发送 worker 默认仅 1 个、串行，在高 QPS 下是显著的性能反模式。仅在进程退出时通过 `Close()` 同步刷出。

### 调用位置

| 路径 | 文件 / 位置 | 触发点 |
|------|-------------|--------|
| 文本补全成功 | `service/text_quota.go` → `PostTextConsumeQuota` 末尾 | `RecordRelaySample` 之后 |
| Audio 成功 | `service/quota.go` → `PostAudioConsumeQuota` 末尾 | `RecordRelaySample` 之后 |
| WSS 成功 | `service/quota.go` → `PostWssConsumeQuota` 末尾 | `RecordConsumeLog` 之后（该路径原本无 `RecordRelaySample`） |
| 请求失败 | `controller/relay.go` → `Relay()` 的 `newAPIError != nil` 分支 | 每次失败必经 |

调用方直接调用 helper（helper 内部已异步），**不需要再包裹 `gopool.Go`**。

## 前端变更

### Token 创建/编辑表单

在表单中添加可折叠的 "Langfuse 可观测性配置" 区域：
- Langfuse Host（输入框，placeholder: `https://cloud.langfuse.com`）
- Public Key（输入框）
- Secret Key（密码输入框；**编辑时不回填**，placeholder 提示"留空保持不变"）

### 涉及文件

- Token 编辑表单组件：添加 Langfuse 配置折叠区
- Token 类型定义：添加 `langfuse_public_key`, `langfuse_secret_key`, `langfuse_host` 字段
- i18n 翻译文件：添加相关翻译 key（注意 locale 文件为 `{"translation": {...}}` 嵌套结构，key 放在 `translation` 对象内）

## 错误处理

- Langfuse 记录失败时仅打印 warning 日志，不影响 API 请求的正常响应
- 客户端池获取客户端异常时跳过记录
- 异步执行（gopool）确保不阻塞主流程
- SDK 队列满时（`ErrBufferFull`）自动丢弃该条 trace 并记日志，不阻塞调用方
- **进程退出限制**：项目当前通过 `server.Run(":" + port)`（阻塞）启动，**没有 signal 优雅停机钩子**。强杀进程时队列尾部可能丢失少量 trace（SDK 默认 3 秒自动 flush，正常 SIGTERM/Ctrl+C 下影响有限）。如需严格不丢数据，可在 `main.go` 增加 signal handler，在退出前调用 `CloseLangfuse()`——作为可选增强，不在本次必做范围。

## 不做的事情

- 不记录输入/输出内容（用户未选择）
- 不做全局级别 Langfuse 配置（仅 Token 级别）
- 不在请求过程中实时记录（仅请求完成后）
- 不做 OpenTelemetry 集成
- 不对 SecretKey 做静态加密存储（沿用 `token.Key` 明文 + 脱敏模式）
- 不改造 `main.go` 优雅停机（可选增强，见"错误处理"）
