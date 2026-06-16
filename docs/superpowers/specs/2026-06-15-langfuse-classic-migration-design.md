# Langfuse 可观测性配置 — 迁移到 classic 前端

- **日期**：2026-06-15
- **状态**：待实现
- **关联**：`docs/superpowers/specs/2026-06-12-langfuse-integration-design.md`（Langfuse 整体集成初始设计）

## 1. 背景与目标

Langfuse 可观测性功能已在 default 前端的令牌（API Key）编辑抽屉中实现：用户可为每个令牌配置 Langfuse Host / Public Key / Secret Key，用于该令牌调用链路的追踪上报。

后端对该功能的支持已完整就绪：

- 数据模型字段：`model/token.go:31-33`（`LangfusePublicKey` / `LangfuseSecretKey` / `LangfuseHost`）
- 更新逻辑（含「留空保持当前密钥」）：`controller/token.go:306-310`
- 编辑回显脱敏：`controller/token.go:23`（`GetToken` 返回 `GetMaskedLangfuseSecretKey`）
- 创建接收：`controller/token.go:225-227`
- 启用判定：`model/token.go:356`（`LangfuseEnabled`，三字段皆非空才启用）

**但 classic 前端完全没有实现**（`web/classic/src` 下 grep `langfuse` 零结果），导致使用 classic 主题的用户无法配置该功能。

**目标**：将 Langfuse 配置功能补齐到 classic 前端的令牌编辑表单，行为与 default 端保持一致。**这是纯前端改动，后端零改动。**

## 2. 设计决策

### 呈现方式：独立卡片，始终展开

classic 前端没有 default 端的 `Collapsible` 折叠面板，其令牌编辑表单（`EditTokenModal.jsx`）的风格是用多张 `<Card>` 分区：基本信息、额度设置、访问限制。

本设计在「访问限制」卡片之后**新增第 4 张「Langfuse 可观测性」卡片，三个字段始终平铺可见**。

**理由**：default 端默认折叠的 Collapsible 正是用户「看不到该功能」的直接原因（见本次会话前序讨论）。classic 端用始终展开的独立卡片，既贴合现有 Card 分区约定，又避免了再次被折叠藏起来的可用性问题。

### 实现位置：内联（不抽独立组件）

与现有三张 Card 一样内联在 `EditTokenModal.jsx` 同一文件中，保持风格一致性。

## 3. 改动文件

### 3.1 `web/classic/src/components/table/tokens/modals/EditTokenModal.jsx`

1. 在「访问限制」`<Card>` 之后新增第 4 张 `<Card>`：
   - 卡片头部：紫色 `Avatar` + 标题「Langfuse 可观测性」+ 描述「为此 API 密钥配置 Langfuse 追踪」
   - 复用现有 Card 头部结构与样式（参考 `EditTokenModal.jsx:595-610` 访问限制卡片写法）

2. 卡片体内三个 `Form.Input` 字段（置于 `<Row><Col span={24}>` 中，与现有字段一致）：

   | field | label | placeholder | 备注 |
   |-------|-------|-------------|------|
   | `langfuse_host` | `Langfuse Host` | `https://cloud.langfuse.com` | extraText: `Langfuse server URL` |
   | `langfuse_public_key` | `Langfuse Public Key` | `pk-...` | — |
   | `langfuse_secret_key` | `Langfuse Secret Key` | 编辑：`留空保持当前`；新建：`sk-...` | 使用 Semi `mode="password"`（原生显示/隐藏切换） |

3. `getInitValues()` 中补充三个字段的初始空值：
   ```js
   langfuse_host: '',
   langfuse_public_key: '',
   langfuse_secret_key: '',
   ```

4. **`submit` 函数无需修改**：它已通过 `let { tokenCount: _tc, ...localInputs } = values` 整体解构提交，新增的三个字符串字段会自动包含在 POST/PUT 请求体中，后端创建/更新接口均已接收这三个字段。

### 3.2 classic i18n — 8 个 locale 文件（`web/classic/src/i18n/locales/*.json`）

需添加的翻译 key（从 default 对应 locale 复制）：

1. `Langfuse Observability`
2. `Configure Langfuse tracing for this API key`
3. `Langfuse Host`
4. `Langfuse server URL`
5. `Langfuse Public Key`
6. `Langfuse Secret Key`
7. `Enter your Langfuse secret key`
8. `Leave empty to keep the current secret key`
9. `Leave empty to keep current`（编辑时 placeholder，可能 classic 已有，实现时检查去重）

**locale 映射策略**：

| classic locale | 翻译来源 |
|----------------|----------|
| `en.json` | default `en.json`（key = value） |
| `zh.json` | default `zh.json`（简体） |
| `fr.json` / `ja.json` / `ru.json` / `vi.json` | default 对应 locale |
| `zh-CN.json` | 简体中文，取 default `zh.json` 翻译 |
| `zh-TW.json` | 繁体中文，将 default `zh.json` 翻译转繁体 |

参考译值（default）：

- 英文：`"Langfuse Host": "Langfuse Host"`，`"Langfuse Observability": "Langfuse Observability"`，`"Leave empty to keep current": "Leave empty to keep current"`
- 中文：`"Langfuse Host": "Langfuse 地址"`，`"Langfuse Observability": "Langfuse 可观测性"`，`"Leave empty to keep current": "留空保持不变"`

## 4. 字段行为（严格复刻 default，保证两端一致）

| 字段 | 新建时 | 编辑时 |
|------|--------|--------|
| `langfuse_host` | 空 | 回显原值 |
| `langfuse_public_key` | 空 | 回显原值 |
| `langfuse_secret_key` | 空 | 回显**脱敏值**（后端 `GetToken` 已脱敏返回） |

**「留空保持当前密钥」机制**：编辑时后端 `GetToken` 返回脱敏后的 secret key；用户若不清空、不改，提交脱敏值；`UpdateToken` 判断「提交值 == 脱敏值」则跳过更新（`controller/token.go:307-309`），原密钥保留。用户若清空提交（空值），同样满足 `token.LangfuseSecretKey != ""` 的前置条件被跳过，原密钥保留。两种情况都不会误覆盖。

**校验**：三个字段全部可选，不强制必填（与 default 一致）。是否启用 Langfuse 由后端 `LangfuseEnabled()`（三字段皆非空）判定，前端不做必填校验。

## 5. 边界与错误处理

- 无新增校验规则，无新增错误分支。
- 新建、编辑两条提交路径均自动包含新字段，无需特殊处理。
- 不加「测试连接」按钮（default 没有，后端也无此接口）。
- default 主题不应受任何影响（本次不触碰 `web/default/`）。

## 6. 验证方式

1. **可见性**：切到 classic 主题，打开「添加令牌」抽屉 → 滚动可见第 4 张「Langfuse 可观测性」卡片，三个字段始终展开可见。
2. **新建**：填入测试配置（host / public key / secret key）创建令牌 → 查 `one-api.db` 的 `tokens` 表，对应字段已写入。
3. **编辑-保留密钥**：编辑该令牌 → secret key 显示脱敏值，直接提交（不改）→ 查库原密钥未变。
4. **编辑-清空**：编辑时清空 secret key 提交 → 查库原密钥仍保留。
5. **i18n**：切换到中文/英文等语言 → 卡片标题与字段文案正确显示。
6. **回归 default**：切回 default 主题 → 令牌编辑功能仍正常。
7. **编译**：`cd web && bun install && cd web/classic && bun run build` 成功，产物可被后端 `go:embed` 嵌入。

## 7. 不在范围内（YAGNI）

- 不新增后端逻辑或接口（已完备）。
- 不抽取 Langfuse 为独立组件（保持与现有 Card 一致）。
- 不加测试连接 / 连通性校验。
- 不改 default 前端。
