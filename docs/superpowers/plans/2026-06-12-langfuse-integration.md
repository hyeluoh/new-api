# Langfuse 集成实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **修订说明（2026-06-13）**：本计划已根据代码现状与 `langfuse-go@v0.1.0` 真实 API 校准，修复了原版会导致编译失败的 SDK 类型/签名错误、函数名错误、context key 类型错误，以及"每请求 Flush"性能反模式、SecretKey 加密承诺无法实现等问题。SecretKey 改为明文存库 + 脱敏返回（与 `token.Key` 一致），编辑更新改为"空值不更新"。

**Goal:** 在 API 请求完成后将元数据记录到 Langfuse，按 Token 粒度配置各自的 Langfuse 项目。

**Architecture:** Token 模型新增 Langfuse 配置字段（PublicKey/SecretKey/Host），通过 Gin context 传递到 relay 层。使用 langfuse-go SDK 的客户端池管理器缓存连接（SDK 客户端类型 `*langfuse.Langfuse`），在请求完成后（成功或失败路径）异步创建 trace + generation 并发送。

**Tech Stack:** Go, langfuse-go SDK (`github.com/git-hulk/langfuse-go` v0.1.0), GORM, Gin, React 19 + TypeScript + Zod

---

### Task 1: 添加 langfuse-go 依赖

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: 安装 langfuse-go SDK**

```bash
go get github.com/git-hulk/langfuse-go
```

- [ ] **Step 2: 验证依赖安装成功**

```bash
go mod tidy
grep "langfuse-go" go.mod
```

Expected: 输出包含 `github.com/git-hulk/langfuse-go`

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: 添加 langfuse-go SDK 依赖"
```

---

### Task 2: Token 模型添加 Langfuse 字段

**Files:**
- Modify: `model/token.go:14-32` (Token struct)
- Modify: `model/token.go:297-298` (Update Select 字段列表)

> 注：`AddToken` 的 cleanToken 构造在 `controller/token.go`（非 `model/token.go`），归入 Task 5。

- [ ] **Step 1: 在 Token struct 添加三个字段**

在 `model/token.go` 的 `Token` struct 中，`CrossGroupRetry` 字段之后、`DeletedAt` 之前添加：

```go
	LangfusePublicKey string         `json:"langfuse_public_key" gorm:"type:text"`
	LangfuseSecretKey string         `json:"langfuse_secret_key" gorm:"type:text"`
	LangfuseHost      string         `json:"langfuse_host" gorm:"type:text"`
```

完整的 Token struct 应为：

```go
type Token struct {
	Id                 int            `json:"id"`
	UserId             int            `json:"user_id" gorm:"index"`
	Key                string         `json:"key" gorm:"type:varchar(128);uniqueIndex"`
	Status             int            `json:"status" gorm:"default:1"`
	Name               string         `json:"name" gorm:"index" `
	CreatedTime        int64          `json:"created_time" gorm:"bigint"`
	AccessedTime       int64          `json:"accessed_time" gorm:"bigint"`
	ExpiredTime        int64          `json:"expired_time" gorm:"bigint;default:-1"` // -1 means never expired
	RemainQuota        int            `json:"remain_quota" gorm:"default:0"`
	UnlimitedQuota     bool           `json:"unlimited_quota"`
	ModelLimitsEnabled bool           `json:"model_limits_enabled"`
	ModelLimits        string         `json:"model_limits" gorm:"type:text"`
	AllowIps           *string        `json:"allow_ips" gorm:"default:''"`
	UsedQuota          int            `json:"used_quota" gorm:"default:0"` // used quota
	Group              string         `json:"group" gorm:"default:''"`
	CrossGroupRetry    bool           `json:"cross_group_retry"` // 跨分组重试，仅auto分组有效
	LangfusePublicKey  string         `json:"langfuse_public_key" gorm:"type:text"`
	LangfuseSecretKey  string         `json:"langfuse_secret_key" gorm:"type:text"`
	LangfuseHost       string         `json:"langfuse_host" gorm:"type:text"`
	DeletedAt          gorm.DeletedAt `gorm:"index"`
}
```

- [ ] **Step 2: 更新 Update() 方法的 Select 字段列表**

在 `model/token.go` 第 297-298 行，`Update()` 方法的 `Select()` 调用中追加三个新字段：

```go
	err = DB.Model(token).Select("name", "status", "expired_time", "remain_quota", "unlimited_quota",
		"model_limits_enabled", "model_limits", "allow_ips", "group", "cross_group_retry",
		"langfuse_public_key", "langfuse_secret_key", "langfuse_host").Updates(token).Error
```

- [ ] **Step 3: 在 Token struct 添加 Langfuse 辅助方法**

在 `model/token.go` 中 `GetModelLimitsMap` 方法（第 343 行）之后添加：

```go
// LangfuseEnabled 三个字段全部非空时启用 Langfuse 追踪
func (token *Token) LangfuseEnabled() bool {
	return token.LangfusePublicKey != "" && token.LangfuseSecretKey != "" && token.LangfuseHost != ""
}

// GetMaskedLangfuseSecretKey 返回脱敏后的 SecretKey（仅保留最后 4 位，其余用 *），用于响应脱敏
func (token *Token) GetMaskedLangfuseSecretKey() string {
	if token.LangfuseSecretKey == "" {
		return ""
	}
	if len(token.LangfuseSecretKey) <= 4 {
		return strings.Repeat("*", len(token.LangfuseSecretKey))
	}
	return strings.Repeat("*", len(token.LangfuseSecretKey)-4) + token.LangfuseSecretKey[len(token.LangfuseSecretKey)-4:]
}

// 注：编辑更新策略为"前端不回填 SecretKey、后端空值不更新"（见 Task 5），
// 因此不需要 IsMaskedSecretKey 这类脱敏格式判定函数。
```

- [ ] **Step 4: 验证编译通过**

```bash
go build ./...
```

Expected: 编译成功，无错误

- [ ] **Step 5: Commit**

```bash
git add model/token.go
git commit -m "feat(model): Token 添加 Langfuse 配置字段"
```

---

### Task 3: 创建 Langfuse 客户端池管理器

**Files:**
- Create: `common/langfuse.go`

- [ ] **Step 1: 创建 `common/langfuse.go`**

> 关键点（已对照 `langfuse-go@v0.1.0` 真实 API）：
> - SDK 客户端类型是 `*langfuse.Langfuse`（**不是** `LangfuseClient`）。
> - `langfuse.NewClient(host, publicKey, secretKey)` **不返回 error**。
> - `trace.StartGeneration()` 返回 `*traces.Observation`。
> - **不要在每个 trace 后调用 `client.Flush()`**：SDK 内部已按 100 条/批或 3 秒自动批量；每请求 Flush 会退化为每条一次 HTTP，是性能反模式。
> - **退出时必须 `client.Close()`** 才能刷出队列尾部数据。

```go
package common

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"
	"sync"

	"github.com/bytedance/gopkg/util/gopool"
	langfuse "github.com/git-hulk/langfuse-go"
	"github.com/git-hulk/langfuse-go/pkg/traces"
)

// LangfuseConfig 存储 Langfuse 连接配置
type LangfuseConfig struct {
	PublicKey string
	SecretKey string
	Host      string
}

// LangfuseTraceData 存储需要记录到 Langfuse 的请求元数据
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
	UseTimeMs        int64
	Quota            int64
	Success          bool
	StatusCode       int
	ErrorMessage     string
}

// langfuseManager 管理 Langfuse 客户端实例的缓存池
type langfuseManager struct {
	mu      sync.RWMutex
	clients map[string]*langfuse.Langfuse
}

var langfuseManagerInstance *langfuseManager
var langfuseOnce sync.Once

// GetLangfuseManager 获取 Langfuse 管理器单例
func GetLangfuseManager() *langfuseManager {
	langfuseOnce.Do(func() {
		langfuseManagerInstance = &langfuseManager{
			clients: make(map[string]*langfuse.Langfuse),
		}
	})
	return langfuseManagerInstance
}

// clientKey 生成客户端缓存 key（对三元组做 sha256，不存明文 secret）
func clientKey(publicKey, secretKey, host string) string {
	h := sha256.New()
	h.Write([]byte(publicKey + secretKey + host))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// GetClient 获取或创建 Langfuse 客户端。
// 注意：langfuse.NewClient 不返回 error。
func (m *langfuseManager) GetClient(publicKey, secretKey, host string) *langfuse.Langfuse {
	key := clientKey(publicKey, secretKey, host)

	m.mu.RLock()
	if client, ok := m.clients[key]; ok {
		m.mu.RUnlock()
		return client
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check
	if client, ok := m.clients[key]; ok {
		return client
	}

	client := langfuse.NewClient(host, publicKey, secretKey)
	m.clients[key] = client
	return client
}

// RecordTrace 异步记录请求元数据到 Langfuse。
// 内部使用 gopool 异步执行，调用方无需再包一层 gopool.Go。
func RecordTrace(config LangfuseConfig, data LangfuseTraceData) {
	if config.PublicKey == "" || config.SecretKey == "" || config.Host == "" {
		return
	}

	gopool.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				SysLog(fmt.Sprintf("langfuse RecordTrace panic: %v", r))
			}
		}()

		mgr := GetLangfuseManager()
		client := mgr.GetClient(config.PublicKey, config.SecretKey, config.Host)

		ctx := context.Background()

		tags := []string{data.ModelName}
		if data.IsStream {
			tags = append(tags, "stream")
		} else {
			tags = append(tags, "non-stream")
		}
		if data.Success {
			tags = append(tags, "success")
		} else {
			tags = append(tags, "error")
		}

		traceName := data.ModelName
		if traceName == "" {
			traceName = "unknown-model"
		}

		trace := client.StartTrace(ctx, traceName)
		trace.UserID = strconv.Itoa(data.UserID)
		trace.Metadata = map[string]interface{}{
			"token_name": data.TokenName,
			"channel_id": data.ChannelID,
			"group":      data.Group,
			"request_id": data.RequestID,
		}
		trace.Tags = tags

		// StartGeneration 返回 *traces.Observation
		generation := trace.StartGeneration("completion")
		generation.Model = data.ModelName
		generation.Usage = traces.Usage{
			Input:  data.PromptTokens,
			Output: data.CompletionTokens,
			Total:  data.TotalTokens,
			Unit:   traces.UnitTokens,
		}
		generation.Metadata = map[string]interface{}{
			"latency_ms": data.UseTimeMs,
			"quota_cost": data.Quota,
		}

		if !data.Success {
			generation.StatusMessage = data.ErrorMessage
		}
		generation.End()

		trace.Output = map[string]interface{}{
			"success":     data.Success,
			"status_code": data.StatusCode,
		}

		trace.End()
		// 不调用 client.Flush()：SDK 内部按 100 条/批或 3 秒自动批量发送，
		// 每请求 Flush 会退化为每条一次 HTTP，是性能反模式。仅在进程退出时 Close()。
	})
}

// CloseLangfuse 关闭所有缓存的 Langfuse 客户端，刷出队列尾部数据。
// 应在进程退出前调用——SDK 必须通过 Close() 同步刷出未发送的 trace。
func CloseLangfuse() {
	if langfuseManagerInstance == nil {
		return
	}
	mgr := GetLangfuseManager()
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	for _, client := range mgr.clients {
		if err := client.Close(); err != nil {
			SysLog(fmt.Sprintf("langfuse client close error: %v", err))
		}
	}
	mgr.clients = make(map[string]*langfuse.Langfuse)
}
```

- [ ] **Step 2: 验证编译通过**

```bash
go build ./...
```

Expected: 编译成功

- [ ] **Step 3: Commit**

```bash
git add common/langfuse.go
git commit -m "feat(common): 添加 Langfuse 客户端池管理器"
```

---

### Task 4: 中间件传递 Langfuse 配置到 Context

**Files:**
- Modify: `middleware/auth.go:409-439` (SetupContextForToken)

- [ ] **Step 1: 在 SetupContextForToken 中添加 Langfuse context**

在 `middleware/auth.go` 的 `SetupContextForToken` 函数中（第 428 行），在 `common.SetContextKey(c, constant.ContextKeyTokenCrossGroupRetry, token.CrossGroupRetry)` 之后、`if len(parts) > 1` 之前添加：

```go
	// Langfuse 可观测性配置
	c.Set("langfuse_public_key", token.LangfusePublicKey)
	c.Set("langfuse_secret_key", token.LangfuseSecretKey)
	c.Set("langfuse_host", token.LangfuseHost)
```

- [ ] **Step 2: 验证编译通过**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add middleware/auth.go
git commit -m "feat(middleware): TokenAuth 传递 Langfuse 配置到 context"
```

---

### Task 5: Controller 层处理 Langfuse 字段（创建/更新/脱敏）

**Files:**
- Modify: `controller/token.go:17-24` (buildMaskedTokenResponse 脱敏)
- Modify: `controller/token.go:210-224` (AddToken cleanToken 构造)
- Modify: `controller/token.go:291-302` (UpdateToken 字段赋值)

- [ ] **Step 1: 在 AddToken 的 cleanToken 构造中添加 Langfuse 字段**

在 `controller/token.go` 的 `AddToken` 函数中，cleanToken 构造（第 210-224 行）添加三个字段：

```go
		cleanToken := model.Token{
			UserId:             c.GetInt("id"),
			Name:               token.Name,
			Key:                key,
			CreatedTime:        common.GetTimestamp(),
			AccessedTime:       common.GetTimestamp(),
			ExpiredTime:        token.ExpiredTime,
			RemainQuota:        token.RemainQuota,
			UnlimitedQuota:     token.UnlimitedQuota,
			ModelLimitsEnabled: token.ModelLimitsEnabled,
			ModelLimits:        token.ModelLimits,
			AllowIps:           token.AllowIps,
			Group:              token.Group,
			CrossGroupRetry:    token.CrossGroupRetry,
			LangfusePublicKey:  token.LangfusePublicKey,
			LangfuseSecretKey:  token.LangfuseSecretKey,
			LangfuseHost:       token.LangfuseHost,
		}
```

- [ ] **Step 2: 在 UpdateToken 中添加 Langfuse 字段处理（空值不更新）**

在 `controller/token.go` 的 `UpdateToken` 函数中，`else` 分支（非 statusOnly）的字段赋值部分（约第 291-302 行），在 `cleanToken.CrossGroupRetry = token.CrossGroupRetry` 之后添加：

```go
			cleanToken.Name = token.Name
			cleanToken.ExpiredTime = token.ExpiredTime
			cleanToken.RemainQuota = token.RemainQuota
			cleanToken.UnlimitedQuota = token.UnlimitedQuota
			cleanToken.ModelLimitsEnabled = token.ModelLimitsEnabled
			cleanToken.ModelLimits = token.ModelLimits
			cleanToken.AllowIps = token.AllowIps
			cleanToken.Group = token.Group
			cleanToken.CrossGroupRetry = token.CrossGroupRetry
			// Langfuse 配置更新：前端编辑时不回填 SecretKey（见 Task 7/8），
			// 空值表示不更新，避免把脱敏占位值回写覆盖真实密钥。
			cleanToken.LangfusePublicKey = token.LangfusePublicKey
			if token.LangfuseSecretKey != "" {
				cleanToken.LangfuseSecretKey = token.LangfuseSecretKey
			}
			cleanToken.LangfuseHost = token.LangfuseHost
```

- [ ] **Step 3: 在 `buildMaskedTokenResponse` 中脱敏 SecretKey**

`controller/token.go:17-24` 的 `buildMaskedTokenResponse` 是所有 Token 返回（详情 + 列表）的唯一脱敏出口——列表接口 `GetAllTokens`/`SearchTokens` 经 `buildMaskedTokenResponses` 复用它。它复制整个 Token 结构体后只覆盖 `Key`，因此**只需追加一行**对 `LangfuseSecretKey` 的脱敏，即可覆盖全部返回路径：

```go
func buildMaskedTokenResponse(token *model.Token) *model.Token {
	if token == nil {
		return nil
	}
	maskedToken := *token
	maskedToken.Key = token.GetMaskedKey()
	maskedToken.LangfuseSecretKey = token.GetMaskedLangfuseSecretKey() // 新增
	return &maskedToken
}
```

> 无需在 `GetAllTokens`/`SearchTokens` 中单独写循环——它们已经走 `buildMaskedTokenResponses` → `buildMaskedTokenResponse`，自动覆盖。新增的 `LangfusePublicKey`/`LangfuseHost` 字段会被 JSON 自动序列化，无需额外处理。

- [ ] **Step 4: 验证编译通过**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add controller/token.go
git commit -m "feat(controller): Token 创建/更新/查询处理 Langfuse 字段及脱敏"
```

---

### Task 6: 在请求完成后调用 Langfuse 记录

这是最核心的集成步骤。

**Files:**
- Create: `common/langfuse_helper.go` (从 context 提取配置并调用 RecordTrace 的辅助函数)
- Modify: `service/text_quota.go` `PostTextConsumeQuota` 末尾 (~476-478)
- Modify: `service/quota.go` `PostAudioConsumeQuota` 末尾 (~377-379) + `PostWssConsumeQuota` 末尾 (~255)
- Modify: `controller/relay.go` `Relay()` 的 `newAPIError != nil` 分支 (~243-247)

> 关键校准（已对照代码现状）：
> - 文本函数名是 **`PostTextConsumeQuota`**（不是 `PostConsumeTextQuota`）。
> - `common.GetContextKeyTime` 第二参数类型是 `constant.ContextKey`，不能传字符串；`is_stream` 用 `constant.ContextKeyIsStream`、起始时间用 `constant.ContextKeyRequestStartTime`。
> - **不要在 common 包重复声明 `type ContextKey string`**，直接引用 `constant` 包（common 已依赖 constant，无循环）。
> - WSS 路径的 usage 是 `*dto.RealtimeUsage`，字段是 `InputTokens`/`OutputTokens`（不是 PromptTokens/CompletionTokens）。
> - `RecordTrace` 内部已异步，调用方**无需再包 `gopool.Go`**。

- [ ] **Step 1: 创建 `common/langfuse_helper.go` 辅助函数**

```go
package common

import (
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
)

// RecordLangfuseTraceFromContext 从 gin context 提取 Langfuse 配置并记录 trace。
// 内部已异步（RecordTrace 用 gopool），调用方无需再包 gopool.Go。
func RecordLangfuseTraceFromContext(c *gin.Context, modelName string, promptTokens int, completionTokens int, totalTokens int, quota int64, success bool, statusCode int, errMsg string) {
	publicKey := c.GetString("langfuse_public_key")
	secretKey := c.GetString("langfuse_secret_key")
	host := c.GetString("langfuse_host")

	if publicKey == "" || secretKey == "" || host == "" {
		return
	}

	startTime := GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
	if startTime.IsZero() {
		startTime = time.Now()
	}
	useTimeMs := time.Since(startTime).Milliseconds()

	RecordTrace(
		LangfuseConfig{
			PublicKey: publicKey,
			SecretKey: secretKey,
			Host:      host,
		},
		LangfuseTraceData{
			RequestID:        c.GetString(RequestIdKey),
			UserID:           c.GetInt("id"),
			TokenName:        c.GetString("token_name"),
			ModelName:        modelName,
			ChannelID:        c.GetInt("channel_id"),
			Group:            c.GetString("group"),
			IsStream:         c.GetBool(string(constant.ContextKeyIsStream)),
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
			UseTimeMs:        useTimeMs,
			Quota:            quota,
			Success:          success,
			StatusCode:       statusCode,
			ErrorMessage:     errMsg,
		},
	)
}

// RecordLangfuseErrorTrace 从 gin context 记录错误 trace（无 token 用量）
func RecordLangfuseErrorTrace(c *gin.Context, modelName string, statusCode int, errMsg string) {
	publicKey := c.GetString("langfuse_public_key")
	secretKey := c.GetString("langfuse_secret_key")
	host := c.GetString("langfuse_host")

	if publicKey == "" || secretKey == "" || host == "" {
		return
	}

	startTime := GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
	if startTime.IsZero() {
		startTime = time.Now()
	}
	useTimeMs := time.Since(startTime).Milliseconds()

	RecordTrace(
		LangfuseConfig{
			PublicKey: publicKey,
			SecretKey: secretKey,
			Host:      host,
		},
		LangfuseTraceData{
			RequestID:    c.GetString(RequestIdKey),
			UserID:       c.GetInt("id"),
			TokenName:    c.GetString("token_name"),
			ModelName:    modelName,
			ChannelID:    c.GetInt("channel_id"),
			Group:        c.GetString("group"),
			IsStream:     c.GetBool(string(constant.ContextKeyIsStream)),
			UseTimeMs:    useTimeMs,
			Success:      false,
			StatusCode:   statusCode,
			ErrorMessage: errMsg,
		},
	)
}
```

- [ ] **Step 2: 在文本补全成功后记录 Langfuse**

在 `service/text_quota.go` 的 `PostTextConsumeQuota` 函数末尾（第 476-478 行已有的 `perfmetrics.RecordRelaySample` 之后），**直接调用 helper**（无需再包 `gopool.Go`，helper 内部已异步）：

```go
	gopool.Go(func() {
		perfmetrics.RecordRelaySample(relayInfo, true, int64(summary.CompletionTokens))
	})
	// 记录 Langfuse trace（helper 内部已异步）
	common.RecordLangfuseTraceFromContext(ctx, summary.ModelName, summary.PromptTokens, summary.CompletionTokens, summary.TotalTokens, int64(summary.Quota), true, 200, "")
}
```

> 注：`summary.TotalTokens` 字段已存在（勿用 `PromptTokens+CompletionTokens` 自行相加）。

- [ ] **Step 3: 在 Audio 成功后记录 Langfuse**

在 `service/quota.go` 的 `PostAudioConsumeQuota` 函数末尾（第 377-379 行已有的 `perfmetrics.RecordRelaySample` 之后）。usage 是 `*dto.Usage`，字段 `PromptTokens`/`CompletionTokens`/`TotalTokens`：

```go
	gopool.Go(func() {
		perfmetrics.RecordRelaySample(relayInfo, true, int64(usage.CompletionTokens))
	})
	// 记录 Langfuse trace（helper 内部已异步）
	common.RecordLangfuseTraceFromContext(ctx, relayInfo.OriginModelName, usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens, int64(quota), true, 200, "")
}
```

- [ ] **Step 4: 在 WSS 成功后记录 Langfuse**

在 `service/quota.go` 的 `PostWssConsumeQuota` 函数末尾（`model.RecordConsumeLog(...)` 之后、函数结束 `}` 之前，约第 255 行）添加。注意：该路径**原本没有** `perfmetrics.RecordRelaySample`，usage 是 `*dto.RealtimeUsage`，字段是 `InputTokens`/`OutputTokens`/`TotalTokens`，函数签名有 `modelName` 参数：

```go
		Group:            relayInfo.UsingGroup,
		Other:            other,
	})
	// 记录 Langfuse trace（该路径原本无 RecordRelaySample；helper 内部已异步）
	common.RecordLangfuseTraceFromContext(ctx, modelName, usage.InputTokens, usage.OutputTokens, usage.TotalTokens, int64(quota), true, 200, "")
}
```

> 编辑前请阅读 `PostWssConsumeQuota`（第 157-256 行）确认 `modelName`、`usage`、`quota`、`ctx` 均在作用域内（它们在函数签名 / 函数体已声明）。

- [ ] **Step 5: 在失败路径记录 Langfuse**

在 `controller/relay.go` 的 `Relay()` 函数中（第 243-247 行），`newAPIError != nil` 分支**已有** `perfmetrics.RecordRelaySample`，在其后追加一行 Langfuse 错误记录（**不要重复调用** RecordRelaySample）：

```go
	if newAPIError != nil {
		gopool.Go(func() {
			perfmetrics.RecordRelaySample(relayInfo, false, 0)
		})
		// 记录 Langfuse 错误 trace（helper 内部已异步）
		common.RecordLangfuseErrorTrace(c, relayInfo.OriginModelName, newAPIError.StatusCode, newAPIError.Error())
	}
```

> 选择 `newAPIError != nil` 分支而非 `processChannelError`：前者每次失败必经，后者受 `ErrorLogEnabled && IsRecordErrorLog(err)` 开关限制会漏报。

- [ ] **Step 6: 验证编译通过**

```bash
go build ./...
```

- [ ] **Step 7: Commit**

```bash
git add common/langfuse_helper.go service/text_quota.go service/quota.go controller/relay.go
git commit -m "feat: 集成 Langfuse trace 记录到请求完成路径"
```

---

### Task 7: 前端 — Token 类型定义和表单 Schema

**Files:**
- Modify: `web/default/src/features/keys/types.ts:25-48` (apiKeySchema)
- Modify: `web/default/src/features/keys/types.ts:85-95` (ApiKeyFormData)
- Modify: `web/default/src/features/keys/lib/api-key-form.ts:29-58` (form schema)
- Modify: `web/default/src/features/keys/lib/api-key-form.ts:66-76` (default values)
- Modify: `web/default/src/features/keys/lib/api-key-form.ts:95-113` (transform to payload)
- Modify: `web/default/src/features/keys/lib/api-key-form.ts:118-139` (transform from API)

- [ ] **Step 1: 更新 TypeScript 类型定义**

在 `web/default/src/features/keys/types.ts` 中：

1. 在 `apiKeySchema` 的 `allow_ips` 之后添加三个字段：

```typescript
export const apiKeySchema = z.object({
  id: z.number(),
  name: z.string(),
  key: z.string(),
  status: z.number(),
  remain_quota: z.number(),
  used_quota: z.number(),
  unlimited_quota: z.boolean(),
  expired_time: z.number(),
  created_time: z.number(),
  accessed_time: z.number(),
  group: z.string().nullish().default(''),
  cross_group_retry: z
    .preprocess((v) => {
      if (v === 1) return true
      if (v === 0) return false
      return v
    }, z.boolean())
    .optional()
    .default(false),
  model_limits_enabled: z.boolean(),
  model_limits: z.string().nullish().default(''),
  allow_ips: z.string().nullish().default(''),
  langfuse_public_key: z.string().optional().default(''),
  langfuse_secret_key: z.string().optional().default(''),
  langfuse_host: z.string().optional().default(''),
})
```

2. 在 `ApiKeyFormData` 接口中添加三个字段：

```typescript
export interface ApiKeyFormData {
  name: string
  remain_quota: number
  expired_time: number
  unlimited_quota: boolean
  model_limits_enabled: boolean
  model_limits: string
  allow_ips: string
  group: string
  cross_group_retry: boolean
  langfuse_public_key?: string
  langfuse_secret_key?: string
  langfuse_host?: string
}
```

- [ ] **Step 2: 更新表单 Schema**

在 `web/default/src/features/keys/lib/api-key-form.ts` 的 `getApiKeyFormSchema` 中，在 `tokenCount` 之后添加：

```typescript
export function getApiKeyFormSchema(t: TFunction) {
  return z
    .object({
      name: z.string().min(1, t('Please enter a name')),
      remain_quota_dollars: z.number().optional(),
      expired_time: z.date().optional(),
      unlimited_quota: z.boolean(),
      model_limits: z.array(z.string()),
      allow_ips: z.string().optional(),
      group: z.string().optional(),
      cross_group_retry: z.boolean().optional(),
      tokenCount: z.number().min(1).optional(),
      langfuse_host: z.string().optional(),
      langfuse_public_key: z.string().optional(),
      langfuse_secret_key: z.string().optional(),
    })
    .superRefine((data, ctx) => {
      // ... 现有的 quota 校验逻辑不变
    })
}
```

- [ ] **Step 3: 更新默认值**

在 `API_KEY_FORM_DEFAULT_VALUES` 中添加：

```typescript
export const API_KEY_FORM_DEFAULT_VALUES: ApiKeyFormValues = {
  name: '',
  remain_quota_dollars: 10,
  expired_time: undefined,
  unlimited_quota: true,
  model_limits: [],
  allow_ips: '',
  group: DEFAULT_GROUP,
  cross_group_retry: true,
  tokenCount: 1,
  langfuse_host: '',
  langfuse_public_key: '',
  langfuse_secret_key: '',
}
```

- [ ] **Step 4: 更新 transformFormDataToPayload**

在 `transformFormDataToPayload` 函数的返回对象中添加：

```typescript
export function transformFormDataToPayload(
  data: ApiKeyFormValues
): ApiKeyFormData {
  return {
    name: data.name,
    remain_quota: data.unlimited_quota
      ? 0
      : parseQuotaFromDollars(data.remain_quota_dollars || 0),
    expired_time: data.expired_time
      ? Math.floor(data.expired_time.getTime() / 1000)
      : -1,
    unlimited_quota: data.unlimited_quota,
    model_limits_enabled: data.model_limits.length > 0,
    model_limits: data.model_limits.join(','),
    allow_ips: data.allow_ips || '',
    group: data.group || '',
    cross_group_retry: data.group === 'auto' ? !!data.cross_group_retry : false,
    langfuse_host: data.langfuse_host || '',
    langfuse_public_key: data.langfuse_public_key || '',
    langfuse_secret_key: data.langfuse_secret_key || '',
  }
}
```

- [ ] **Step 5: 更新 transformApiKeyToFormDefaults（编辑时不回填 SecretKey）**

在 `transformApiKeyToFormDefaults` 函数的返回对象中添加。**`langfuse_secret_key` 始终置空**（编辑时不回填，靠 placeholder 提示，提交空值则后端不更新）：

```typescript
export function transformApiKeyToFormDefaults(
  apiKey: ApiKey
): ApiKeyFormValues {
  return {
    name: apiKey.name,
    remain_quota_dollars: apiKey.unlimited_quota
      ? 0
      : quotaUnitsToDollars(apiKey.remain_quota),
    expired_time:
      apiKey.expired_time > 0
        ? new Date(apiKey.expired_time * 1000)
        : undefined,
    unlimited_quota: apiKey.unlimited_quota,
    model_limits: apiKey.model_limits
      ? apiKey.model_limits.split(',').filter(Boolean)
      : [],
    allow_ips: apiKey.allow_ips || '',
    group: apiKey.group || DEFAULT_GROUP,
    cross_group_retry: !!apiKey.cross_group_retry,
    tokenCount: 1,
    langfuse_host: apiKey.langfuse_host || '',
    langfuse_public_key: apiKey.langfuse_public_key || '',
    langfuse_secret_key: '', // 编辑时不回填，提交空值则后端不更新
  }
}
```

- [ ] **Step 6: Commit**

```bash
git add web/default/src/features/keys/types.ts web/default/src/features/keys/lib/api-key-form.ts
git commit -m "feat(web): Token 类型定义和表单 Schema 添加 Langfuse 字段"
```

---

### Task 8: 前端 — Token 表单 UI 组件

**Files:**
- Modify: `web/default/src/features/keys/components/api-keys-mutate-drawer.tsx:494-576` (Advanced Settings 折叠区之后)

- [ ] **Step 1: 在表单中添加 Langfuse 配置折叠区**

在 `web/default/src/features/keys/components/api-keys-mutate-drawer.tsx` 中：

1. 在 import 中添加 `Activity` 图标（来自 lucide-react）：

```typescript
import { ChevronDown, KeyRound, Settings2, WalletCards, Activity } from 'lucide-react'
```

2. 在 `advancedOpen` state（第 100 行）之后添加一个新的 state：

```typescript
const [langfuseOpen, setLangfuseOpen] = useState(false)
```

3. 在现有 `</Collapsible>` (Advanced Settings) 之后、`</form>` 之前，添加新的 Langfuse 配置折叠区。Secret Key 输入框绑定 `langfuse_secret_key`（编辑时表单值为空，靠 placeholder 提示"留空保持不变"）：

```tsx
            <Collapsible open={langfuseOpen} onOpenChange={setLangfuseOpen}>
              <SideDrawerSection>
                <CollapsibleTrigger
                  render={
                    <button
                      type='button'
                      className='hover:bg-muted/40 flex w-full items-center gap-3 rounded-md py-1.5 text-left transition-colors'
                    />
                  }
                >
                  <SideDrawerSectionHeader
                    className='flex-1'
                    title={t('Langfuse Observability')}
                    description={t('Configure Langfuse tracing for this API key')}
                    icon={<Activity className='size-4' />}
                  />
                  <ChevronDown
                    className={cn(
                      'text-muted-foreground size-4 shrink-0 transition-transform',
                      langfuseOpen && 'rotate-180'
                    )}
                  />
                </CollapsibleTrigger>
                <CollapsibleContent>
                  <div className='flex flex-col gap-4 pt-2'>
                    <FormField
                      control={form.control}
                      name='langfuse_host'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Langfuse Host')}</FormLabel>
                          <FormControl>
                            <Input
                              {...field}
                              placeholder='https://cloud.langfuse.com'
                            />
                          </FormControl>
                          <FormDescription>
                            {t('Langfuse server URL')}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />

                    <FormField
                      control={form.control}
                      name='langfuse_public_key'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Langfuse Public Key')}</FormLabel>
                          <FormControl>
                            <Input
                              {...field}
                              placeholder='pk-...'
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />

                    <FormField
                      control={form.control}
                      name='langfuse_secret_key'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Langfuse Secret Key')}</FormLabel>
                          <FormControl>
                            <Input
                              {...field}
                              type='password'
                              placeholder={isUpdate ? t('Leave empty to keep current') : 'sk-...'}
                            />
                          </FormControl>
                          <FormDescription>
                            {isUpdate
                              ? t('Leave empty to keep the current secret key')
                              : t('Enter your Langfuse secret key')}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  </div>
                </CollapsibleContent>
              </SideDrawerSection>
            </Collapsible>
```

- [ ] **Step 2: 验证前端构建通过**

```bash
cd web/default && bun run build
```

Expected: 构建成功

- [ ] **Step 3: Commit**

```bash
git add web/default/src/features/keys/components/api-keys-mutate-drawer.tsx
git commit -m "feat(web): Token 表单添加 Langfuse 可观测性配置折叠区"
```

---

### Task 9: 前端 — i18n 翻译

**Files:**
- Modify: `web/default/src/i18n/locales/en.json`
- Modify: `web/default/src/i18n/locales/zh.json`

> 注意：locale 文件是 `{"translation": { ... }}` 单层嵌套结构，所有 key 放在 `translation` 对象内（保持字母排序）。手写时注意层级；`bun run i18n:sync` 也会按英文源串补齐。

- [ ] **Step 1: 在英文翻译文件的 `translation` 对象内添加 key**

```json
"Langfuse Host": "Langfuse Host",
"Langfuse Observability": "Langfuse Observability",
"Langfuse Public Key": "Langfuse Public Key",
"Langfuse Secret Key": "Langfuse Secret Key",
"Langfuse server URL": "Langfuse server URL",
"Configure Langfuse tracing for this API key": "Configure Langfuse tracing for this API key",
"Enter your Langfuse secret key": "Enter your Langfuse secret key",
"Leave empty to keep the current secret key": "Leave empty to keep the current secret key",
"Leave empty to keep current": "Leave empty to keep current"
```

- [ ] **Step 2: 在中文翻译文件的 `translation` 对象内添加 key**

```json
"Langfuse Host": "Langfuse 地址",
"Langfuse Observability": "Langfuse 可观测性",
"Langfuse Public Key": "Langfuse 公钥",
"Langfuse Secret Key": "Langfuse 密钥",
"Langfuse server URL": "Langfuse 服务器地址",
"Configure Langfuse tracing for this API key": "为此 API 密钥配置 Langfuse 追踪",
"Enter your Langfuse secret key": "输入您的 Langfuse 密钥",
"Leave empty to keep the current secret key": "留空以保留当前密钥",
"Leave empty to keep current": "留空保持不变"
```

- [ ] **Step 3: 运行 i18n 同步**

```bash
cd web/default && bun run i18n:sync
```

- [ ] **Step 4: 验证前端构建通过**

```bash
cd web/default && bun run build
```

- [ ] **Step 5: Commit**

```bash
git add web/default/src/i18n/
git commit -m "feat(web): 添加 Langfuse 相关 i18n 翻译"
```

---

### Task 10: 集成验证

- [ ] **Step 1: 全量编译**

```bash
go build ./...
```

Expected: 编译成功

- [ ] **Step 2: 前端构建**

```bash
cd web/default && bun run build
```

Expected: 构建成功

- [ ] **Step 3: 启动服务进行手动验证**

```bash
go run main.go
```

验证步骤：
1. 创建一个 Token，填写 Langfuse 配置（Host / Public Key / Secret Key）
2. 编辑该 Token，确认 Langfuse 配置正确显示（Secret Key 输入框为空 + placeholder "留空保持不变"）
3. 不修改 Secret Key 直接保存，确认 Secret Key 不被清空（DB 中仍是原值）
4. 使用该 Token 发送一个文本补全 API 请求
5. 检查 Langfuse 面板是否收到 trace（最多等 3 秒，SDK 批量发送周期）
6. 发送一个会失败的请求（如配额不足 / 无效模型），确认 Langfuse 收到 error trace
7. 检查服务日志无 Langfuse 相关 panic / error

- [ ] **Step 4: （可选增强）进程退出时刷出 Langfuse 数据**

项目当前通过 `server.Run()` 启动，无 signal 优雅停机钩子。强杀进程时队列尾部可能丢失少量 trace（SDK 默认 3 秒自动 flush，影响有限）。如需严格不丢，可在 `main.go` 增加 signal handler，在退出前调用 `common.CloseLangfuse()`——作为可选增强，不在必做范围。示例：

```go
// 在 main.go 的 server.Run 之前/合适位置
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
go func() {
	<-sigCh
	common.CloseLangfuse()
	os.Exit(0)
}()
```

> 注意：`server.Run()` 是阻塞调用，上述简化版在收到信号后直接退出；若需先优雅关闭 HTTP 连接再 Close，需改用 `http.Server` + `Shutdown()`，改动较大，按需评估。

- [ ] **Step 5: 最终 Commit**

如果有任何修复：

```bash
git add -A
git commit -m "fix: 集成验证修复"
```
