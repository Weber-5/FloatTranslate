# 06 — LLM Protocol

## 1. Provider Interface

业务层只依赖抽象能力：

- Complete
- Stream
- TestConnection
- Capabilities

Provider adapter 负责 vendor-specific：请求字段、thinking 开关、JSON mode、reasoning 字段、错误映射。

## 2. Provider Modes

### DeepSeek Preset

默认：
- Base URL：由预设提供，但用户可修改。
- Translation Model：`deepseek-flash`。
- Chat Model：`deepseek-flash`。

### OpenAI-Compatible

用户配置：Base URL + API Key + Model。

兼容性是“best effort”；Provider capability discovery/known capability table 决定哪些高级字段可用。

## 3. Translation Prompt 规则

系统 Prompt 必须强调：

- 仅英译中。
- 输出必须匹配指定 JSON schema。
- 不输出解释性前后缀。
- Translation thinking 关闭。
- Terminology 是硬约束。
- Protected spans 不可修改。
- Custom Translation Prompt 只能追加偏好，不能覆盖 schema 和 terminology。

## 4. Word Structured Output

逻辑结构：

```text
word
lemma
phonetic_uk
phonetic_us
parts_of_speech[]
  part
  meanings[]
synonyms[]
inflections[]
```

规则：

- 不生成例句。
- meanings 为中文。
- synonyms 尽量是英文 lemma/word。
- IPA 只作为文本；音频由本地 TTS。

## 5. Text Structured Output

逻辑结构：

- `source_markdown`
- `translated_markdown`
- `segments[]`
  - `source`
  - `translation`

原文 token 点击能力来自本地 tokenizer，不依赖模型返回 token map。

## 6. Schema Validation

流程：

1. Provider 返回 raw response。
2. Adapter 提取 JSON payload。
3. Go 使用本地 JSON Schema validate。
4. 若失败，构造一次“修复为指定 schema”的请求。
5. 第二次 validate。
6. 再失败 → `STRUCTURED_OUTPUT_INVALID`。

未经验证的对象不得进入前端和数据库正式结果字段。

## 7. Translation Pipeline

`input → language detect → classify → normalize → terminology/protected span → cache → provider → validate/repair → restore span → persist → response`

## 8. Long Text Chunking

优先切分顺序：

1. Markdown block boundary。
2. Paragraph。
3. Sentence。
4. 仅在必要时 token-budget fallback。

每块保留序号，返回后按原序重组。Protected span placeholder 必须在 chunk 生命周期内保持唯一。

## 9. Chat Context Manager

### 顺序

`System Prompt → Global Context → Conversation Context → Compact Summary → Recent Messages`

### 默认参数

- configured max context：1,000,000 tokens。
- auto compact：on。
- threshold：80%。
- max output tokens：用户设置。

### 有效上限

`effective_context_limit = min(user_config, provider_capability_if_known)`

若 clamp，UI 必须显示提示。

## 10. Compact

触发：

- 手动 `/compact`。
- 自动达到 threshold。

原则：

- 只压缩旧消息的“请求上下文表示”。
- SQLite 中原始消息不删除。
- Global/Conversation Context 不压缩。
- Compact Summary 应包含：事实、定义、决策、未完成事项、用户明确偏好，避免无意义对话噪声。

## 11. Thinking

- 仅 Chat。
- 默认 off。
- Provider 支持时发送对应控制字段。
- 返回 reasoning 时通过 SSE `reasoning.delta`。
- UI 默认折叠 reasoning。
- 翻译固定关闭。

### 11.1 翻译关闭思考的字段（按官方文档核对）

DeepSeek 官方 API（`https://api.deepseek.com`，OpenAI 格式）的思考开关是**对象形式**，且**默认打开**、effort 默认 `high`：

```json
{"thinking": {"type": "disabled"}}
```

官方文档另有两种格式（Anthropic 格式无独立开关；Responses API 用 `{"reasoning":{"effort":"none"}}`），以及强度控制 `reasoning_effort`。注意 `enable_thinking` **不是** DeepSeek 官方字段（它属于 Qwen/DashScope 系）。

因此翻译与 compact 请求同时下发三种"关闭"方言，覆盖 DeepSeek 官方、Qwen 系与 vLLM/SGLang 部署：

| 字段 | 适用 |
|---|---|
| `enable_thinking: false` | Qwen / DashScope / SiliconFlow 等 |
| `thinking: {"type": "disabled"}` | DeepSeek 官方（OpenAI 格式），默认即打开故必须显式关闭 |
| `chat_template_kwargs: {"thinking": false}` | vLLM / SGLang 部署的混合推理模型 |

若端点对新增字段返回 400，则**自动降级重试一次**（去掉后两个方言，保留 `enable_thinking:false`），确保不会把原本可用的配置弄坏。响应结构（字段名与长度，不含内容）会写入后端日志；若仍返回 `reasoning_content`，会记录 WARN，可据此判定思考是否真的关闭。

思考模式还会忽略 `temperature` / `presence_penalty` / `frequency_penalty`（不报错、也不生效），关闭后 `temperature` 才会按请求生效。

## 12. Ask AI

Ask AI 不等同于默认上下文绑定。只有用户主动触发时，才把当前 Word/Text 内容包装为引用插入目标 chat 的 user message/context reference。
