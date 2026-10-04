# 04 — API Contract

`openapi/openapi.yaml` 是机器可读唯一契约。本文件解释语义。

## 1. 基础规则

- Base path：`/api/v1`
- Bind：`127.0.0.1:<dynamic>`
- Auth：`Authorization: Bearer <local-session-token>`
- `/health` 可不鉴权；其余业务接口鉴权。
- JSON：UTF-8。
- 时间：RFC3339 UTC string 或统一 epoch milliseconds；实现阶段必须二选一且全局一致。OpenAPI 本文档选择 RFC3339 string。
- ID：UUID/ULID 均可；实现阶段固定一种，建议 ULID 便于按时间排序。

## 2. 标准错误模型

```json
{
  "error": {
    "code": "PROVIDER_UNAVAILABLE",
    "message": "用户可理解的错误信息",
    "retryable": true,
    "details": {}
  }
}
```

建议错误码：

- `UNAUTHORIZED_LOCAL_SESSION`
- `INVALID_REQUEST`
- `UNSUPPORTED_LANGUAGE`
- `PROVIDER_NOT_CONFIGURED`
- `PROVIDER_CONNECTION_FAILED`
- `PROVIDER_UNAVAILABLE`
- `PROVIDER_CONTEXT_LIMIT`
- `STRUCTURED_OUTPUT_INVALID`
- `TRANSLATION_FAILED`
- `DATABASE_ERROR`
- `MIGRATION_FAILED`
- `NOT_FOUND`
- `CONFLICT`
- `GENERATION_ALREADY_ACTIVE`
- `BACKUP_VERSION_UNSUPPORTED`

## 3. Health / Runtime

### `GET /health`
返回 sidecar readiness、版本、数据库状态，不返回任何 secret。

### `GET /api/v1/runtime/capabilities`
返回 Provider 能力和实际有效 context/output 上限，用于 Settings 展示“配置值 vs 生效值”。

## 4. Provider / Settings

### `GET /api/v1/settings/provider`
仅返回：provider mode、base URL、model 名、`api_key_configured`、masked hint。

### `PUT /api/v1/settings/provider`
保存非 secret 配置；若 body 包含 API Key，则写 Credential Manager。

### `POST /api/v1/settings/provider/test`
测试连接；不得在响应或日志中回显完整 Key。

### `GET /api/v1/settings`
返回非 secret settings。

### `PUT /api/v1/settings`
更新 Theme、Context、Proxy、Prompt、Host preference 等。

## 5. Translation

### `POST /api/v1/translations`
请求：

- `text`
- `force_kind?: word|text`
- `bypass_cache?: boolean`

响应：

- `translation_id`
- `kind`
- `source: model|cache`
- `result`（WordTranslation 或 TextTranslation）
- `model`
- `created_at`

后端自动完成 language check、分类、NLP、术语、protected span、cache 和 schema validation。

### `POST /api/v1/translations/{id}/retranslate`
强制绕过 cache；成功后更新当前历史结果和 cache。

### `GET /api/v1/translations/{id}`
获取已保存翻译。

## 6. History

### `GET /api/v1/history`
支持：pagination、kind、query、sort。

### `DELETE /api/v1/history/{id}`
删除单条历史，不影响 vocabulary。

### `DELETE /api/v1/history`
清空历史。

## 7. Vocabulary

### `GET /api/v1/vocabulary`
query + sort by saved_at。

### `PUT /api/v1/vocabulary/{lemma}`
幂等收藏；若已存在更新 last_viewed_at。

### `DELETE /api/v1/vocabulary/{lemma}`
删除收藏。

## 8. Terminology

- `GET /api/v1/terminology`
- `POST /api/v1/terminology`
- `PUT /api/v1/terminology/{id}`
- `DELETE /api/v1/terminology/{id}`

source term case-insensitive unique。

## 9. Tabs

- `GET /api/v1/tabs`
- `PUT /api/v1/tabs`：批量持久化顺序和 active tab。

频繁 UI 排序可在前端 debounce 后写入。

## 10. Chats

- `GET /api/v1/chats`
- `POST /api/v1/chats`
- `PATCH /api/v1/chats/{id}`：rename/context。
- `DELETE /api/v1/chats/{id}`
- `GET /api/v1/chats/{id}/messages`
- `DELETE /api/v1/chats/{id}/messages`：实现 `/clear`，保留 conversation context。

## 11. Chat Streaming

### `POST /api/v1/chats/{id}/generations`

请求：

- `content`
- `thinking: boolean`
- `attachments/context_reference?`：1.0 只用于 Ask AI 的文本引用，不做文件。

响应：`text/event-stream`

事件：

- `generation.started`
- `reasoning.delta`
- `content.delta`
- `generation.completed`
- `generation.error`
- `generation.cancelled`

每个事件至少包含：

- `generation_id`
- `seq`
- `data`

### `POST /api/v1/chats/{id}/generations/{generation_id}/cancel`
停止生成。

### `POST /api/v1/chats/{id}/regenerate`
重新生成最后一条 assistant answer，返回 SSE。

## 12. Context / Commands

### `POST /api/v1/chats/{id}/compact`
手动 compact。

### `GET/PUT /api/v1/context/global`
管理 Global Context。

### `GET/PUT /api/v1/chats/{id}/context`
管理 Conversation Context。

Slash command 由前端识别命令候选，但执行走后端对应 API/command registry。

## 13. Backup

### `POST /api/v1/backup/export`
接收由 Tauri 文件选择器产生的可信目标路径，Go 负责序列化和写入。

### `POST /api/v1/backup/import`
接收可信源路径，Go 负责版本校验、迁移、merge、deduplicate。

## 14. Data reset

### `POST /api/v1/data/clear-business`
清历史、cache、vocabulary、chats/messages、compact summaries、contexts；保留 API Key 和基础 app settings。

### `POST /api/v1/data/reset-app`
必须由 UI 先二次确认；清数据库、Credential Manager key、settings/context。Tauri 同时清 window-state，随后回 onboarding。
