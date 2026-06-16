# Langfuse 采集输入/输出开关 — 设计

- **日期**：2026-06-16
- **状态**：待实现
- **关联**：
  - `docs/superpowers/specs/2026-06-12-langfuse-integration-design.md`（Langfuse 初始集成）
  - `docs/superpowers/specs/2026-06-15-langfuse-classic-migration-design.md`（classic 迁移）

## 1. 背景与目标

当前 Langfuse 上报**仅含元数据**（用户名、令牌名、模型、token 用量、耗时、额度等），**不采集**请求输入（prompt）与响应输出（response）的文本内容。

用户希望增加一个开关，开启后将 chat/completions 的输入与输出文本上报到 Langfuse trace 的 `Input` / `Output` 字段，便于在 Langfuse 直接查看对话原文用于调试与观测。

**核心约束（经调研确认）：**

| 项 | 现状 |
|----|------|
| Input 来源 | ✅ 可从 `info.Request`（`*dto.GeneralOpenAIRequest`，含 Messages）或 `common.GetBodyStorage(c)` 获取请求体 |
| Output 来源 | ⚠️ 响应经各渠道 `adaptor.DoResponse` 写客户端（流式 SSE / 非流式 JSON），RelayInfo 不存完整 output，需在响应链路累积 |
| 上报点 | ✅ `PostConsumeQuota`（`service/quota.go`）持有 relayInfo，采集的文本经 RelayInfo 传递即可 |
| Langfuse 字段 | ✅ `TraceEntry.Input` / `TraceEntry.Output`（`langfuse-go@v0.1.0/pkg/traces/trace.go:25-26`）原生支持任意类型 |

## 2. 需求（已与用户确认）

- **范围**：仅文本对话（chat/completions）。不覆盖 embedding / 图片 / 音频等。
- **开关粒度**：Token 级（与现有 Langfuse host/key 同级，在令牌编辑页配置）。
- **输入输出控制**：一个开关同时控制输入和输出。
- **默认值**：关闭（隐私保护，避免意外上报用户对话原文）。
- **超长处理**：input / output 各按上限截断（常量，默认 16KB）。
- **前端覆盖**：default 与 classic 两个前端都加。

## 3. 方案

**Output 采集采用「包装 c.Writer 累积原始字节」**：在 `TextHelper` 入口（开关开时）用 wrapper 包 `c.Writer`，所有渠道写给客户端的字节同时累积到 builder；请求结束时取累积的原始字节，做一次轻量解析提取纯文本。

选择理由：唯一能统一覆盖所有渠道、且不在每个适配器里重复累积代码的方式。input 则直接复用现成的 `GetBodyStorage(c)` / `info.Request`。

## 4. 改动清单

### 4.1 后端

#### `model/token.go`
- Token 结构体新增字段（与 `LangfusePublicKey` 同级，第 31-33 行附近）：
  ```go
  LangfuseCollectIO bool `json:"langfuse_collect_io" gorm:"default:false"`
  ```
- `Update()` 的 `Select(...)` 列表（第 300-302 行）追加 `"langfuse_collect_io"`。
- 迁移：GORM AutoMigrate 自动加列，三库（SQLite/MySQL/PostgreSQL）兼容（bool 类型由 GORM 统一处理）。
- `controller/token.go` 的 `UpdateToken`（第 295-310 行）cleanToken 赋值处追加 `cleanToken.LangfuseCollectIO = token.LangfuseCollectIO`；`AddToken`（第 225-227 行）接收该字段。

#### `constant/context_key.go`
- 新增 context key（第 23-25 行 Langfuse key 之后）：
  ```go
  ContextKeyLangfuseCollectIO ContextKey = "langfuse_collect_io"
  ```

#### `middleware/auth.go`
- `SetupContextForToken`（第 442 行 `ContextKeyLangfuseHost` 之后）新增：
  ```go
  common.SetContextKey(c, constant.ContextKeyLangfuseCollectIO, token.LangfuseCollectIO)
  ```

#### `relay/common/relay_info.go`
- RelayInfo 结构体新增字段（第 162 行 `UpstreamRequestBodySize` 附近）：
  ```go
  // Langfuse IO 采集（仅在开启 LangfuseCollectIO 时使用）
  LangfuseInput          string
  LangfuseOutput         string
  langfuseOutputBuilder  *strings.Builder // response writer wrapper 累积用
  ```
  （`langfuseOutputBuilder` 小写不导出，仅供内部 wrapper 使用。）

#### `common/langfuse_io.go`（新文件）
- 纯函数，独立可测：
  - `ExtractInputText(rawBody []byte) string`：从原始请求体解析出 messages 文本。**只取 messages 数组的最后一条**（即本轮用户输入，不含 system prompt 与历史对话），截断到上限。每轮请求 messages 会带完整历史，最后一条才是当前请求真正的内容；这样采集干净、聚焦当前输入，避免 system prompt 占满额度。
  - `ExtractOutputText(rawBytes []byte) string`：从累积的响应字节提取纯文本（解析 SSE `data:` 行，提取 delta content 拼接；非流式则解析 JSON 的 choices[].message.content），截断到上限。
  - `const LangfuseMaxContentBytes = 16 * 1024`
- 所有解析失败均返回空字符串（不影响主流程），内部错误仅记日志。
- 严格使用 `common.Unmarshal`（遵循项目 Rule 1），不直接用 `encoding/json`。

#### `relay/compatible_handler.go`（TextHelper）
- 入口处（开关开且 Langfuse 已配置时）：
  1. 从 `info.Request` 取请求，`ExtractInputText` 得到输入文本，存 `info.LangfuseInput`。
  2. 用 wrapper 包装 `c.Writer`（包装 gin ResponseWriter，写入时同时 `Write` 到 `info.langfuseOutputBuilder`），存原 writer 以便结束时还原。
- `PostTextConsumeQuota` 调用前（响应已写完）：从 builder 取原始字节，`ExtractOutputText` 得到输出文本，存 `info.LangfuseOutput`；还原 writer。

#### `common/langfuse.go`（RecordTrace）
- `LangfuseTraceData` 新增 `Input string` / `Output string` 字段（第 24-40 行结构体）。
- `RecordTrace`（第 167 行 `trace := client.StartTrace` 之后）：当 `data.Input != ""` 时设 `trace.Input = data.Input`；`data.Output != ""` 时设 `trace.Output = data.Output`。Langfuse SDK 的 `TraceEntry.Input/Output` 为 `any` 类型，直接赋字符串即可，`json:"input,omitempty"` 保证空值不上报。

#### `common/langfuse_helper.go`（两个 helper）
- `LangfuseTraceData` 构造时，从 RelayInfo/context 取 `LangfuseInput` / `LangfuseOutput` 填入。
- helper 签名当前接收 `*gin.Context`，需确认能拿到 RelayInfo；若 helper 无法直接访问 RelayInfo，则经 context key 传递（input/output 也存入 context）。

### 4.2 前端 — classic

#### `web/classic/src/components/table/tokens/modals/EditTokenModal.jsx`
- Langfuse 卡片（第 650 行起）的 Secret Key 字段之后新增：
  ```jsx
  <Col span={24}>
    <Form.Switch
      field='langfuse_collect_io'
      label={t('采集输入与输出')}
      extraText={t('开启后会将请求与响应的文本内容上报到 Langfuse，注意隐私')}
    />
  </Col>
  ```
- `getInitValues`（第 73-85 行）新增 `langfuse_collect_io: false`。

### 4.3 前端 — default

#### `web/default/src/features/keys/lib/api-key-form.ts`
- zod schema（第 41-43 行附近）新增 `langfuse_collect_io: z.boolean().optional().default(false)`。
- `defaultValues`（第 90-92 行附近）新增 `langfuse_collect_io: false`。
- 提交转换（第 129-131 行）新增 `langfuse_collect_io: data.langfuse_collect_io ?? false`。
- 回显转换（第 158-160 行）新增 `langfuse_collect_io: apiKey.langfuse_collect_io ?? false`。

#### `web/default/src/features/keys/components/api-keys-mutate-drawer.tsx`
- Langfuse 折叠面板内（第 645 行 `langfuse_secret_key` 字段之后、第 666 `</CollapsibleContent>` 之前）新增 Switch FormField：
  ```tsx
  <FormField
    control={form.control}
    name='langfuse_collect_io'
    render={({ field }) => (
      <FormItem>
        <FormLabel>{t('采集输入与输出')}</FormLabel>
        <FormControl>
          <Switch
            checked={field.value}
            onCheckedChange={field.onChange}
          />
        </FormControl>
        <FormDescription>{t('开启后会将请求与响应的文本内容上报到 Langfuse，注意隐私')}</FormDescription>
      </FormItem>
    )}
  />
  ```
  （使用 default 前端已有的 Switch 组件，参考该文件其他 Switch 用法。）

### 4.4 i18n

两个前端各加 2 个新 key（共 16 处：classic 8 locale + default 6 locale）：
- `采集输入与输出`
- `开启后会将请求与响应的文本内容上报到 Langfuse，注意隐私`

classic 8 locale（en/zh/zh-CN/zh-TW/fr/ja/ru/vi）、default 6 locale（en/zh/fr/ja/ru/vi）。翻译参考已有 Langfuse key 的风格，zh-TW 用繁体。

## 5. 数据流

```
请求带 token（LangfuseCollectIO=true）
  → auth.go SetupContextForToken: 写 ContextKeyLangfuseCollectIO 到 context
  → TextHelper 入口: 开关开 → ExtractInputText 存 info.LangfuseInput；包装 c.Writer
  → 各渠道 DoResponse 写响应: wrapper 同步累积到 builder
  → 响应结束: ExtractOutputText 存 info.LangfuseOutput；还原 writer
  → PostConsumeQuota: RecordLangfuseTraceFromContext 读取 input/output
  → RecordTrace: trace.Input / trace.Output 填充（开关关时为空，不填）
  → 异步上报 Langfuse
```

开关关时：不包装 writer、不提取文本、trace.Input/Output 为空 → 零额外开销，回归现状。

## 6. 错误处理与边界

- **解析失败**：`ExtractInputText` / `ExtractOutputText` 内部 recover，失败返回空字符串，不影响主请求。
- **截断**：input/output 各按 `LangfuseMaxContentBytes`（16KB）截断，防止内存膨胀与上报过大。
- **writer 还原**：TextHelper 末尾必须还原 `c.Writer`（defer），避免影响后续中间件。
- **双层保护**：仅当 `LangfuseCollectIO` 开启 **且** 令牌配置了 Langfuse（host/key 三项齐全）时才采集。
- **开关关零开销**：不创建 builder、不包装 writer，性能与现状一致。

## 7. 测试

- **`common/langfuse_io.go` 纯函数单测**（核心）：
  - `ExtractInputText`：正常 messages、空 body、超长截断、非法 JSON。
  - `ExtractOutputText`：流式 SSE 多 chunk、非流式 JSON、超长截断、非法字节。
- **手动验证**：
  1. 开关切关 → Langfuse trace 无 Input/Output（回归现状）。
  2. 开关开 + 配 Langfuse → trace 含 Input（用户消息）/ Output（模型回复）。
  3. 超长输出 → 截断到 16KB，无 OOM。
  4. classic 与 default 卡片均显示开关，默认关。
  5. 流式与非流式两种请求均正常采集 output。
- **编译**：`go build ./...`、classic 与 default `bun run build` 均通过。

## 8. 不在范围（YAGNI）

- 不支持 embedding / 图片 / 音频 / rerank 等非文本对话渠道。
- 不做输入/输出分开的两个开关（一个开关同控）。
- 不做全局系统设置开关（仅 token 级）。
- 截断上限为代码常量，不做配置化（如需可后续加系统设置）。

## 9. 风险

- **隐私**：开启会把用户对话原文发往 Langfuse（可能为外部服务）。默认关闭 + extraText 明确提示，由用户自行决定。
- **性能**：writer wrapper 仅开关开时启用；累积受 16KB 上限约束，内存成本可控。
- **兼容性**：bool 字段 + GORM AutoMigrate，三库均兼容；新增 context key 不影响既有逻辑。
