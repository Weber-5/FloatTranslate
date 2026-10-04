# 05 — Data Model

## 1. 原则

- SQLite 由 Go 独占。
- WAL 模式可作为默认候选；实现时通过测试确认。
- 所有 schema 变更使用版本化 migration。
- Migration 必须向前执行并保留用户数据。
- Migration 失败时禁止业务写入。

## 2. 逻辑表

### `app_meta`

- `key TEXT PRIMARY KEY`
- `value TEXT NOT NULL`

记录 schema/version metadata。

### `settings`

- `key TEXT PRIMARY KEY`
- `value_json TEXT NOT NULL`
- `updated_at TEXT NOT NULL`

仅存非 secret settings。

### `translation_cache`

- `cache_key TEXT PRIMARY KEY`
- `kind TEXT NOT NULL` (`word|text`)
- `normalized_input TEXT NOT NULL`
- `model TEXT NOT NULL`
- `config_hash TEXT NOT NULL`
- `result_json TEXT NOT NULL`
- `created_at TEXT NOT NULL`
- `updated_at TEXT NOT NULL`
- `last_used_at TEXT NOT NULL`

### `translation_history`

- `id TEXT PRIMARY KEY`
- `kind TEXT NOT NULL`
- `input_text TEXT NOT NULL`
- `normalized_input TEXT NOT NULL`
- `result_json TEXT NOT NULL`
- `source TEXT NOT NULL` (`model|cache`)
- `model TEXT NOT NULL`
- `created_at TEXT NOT NULL`
- `last_viewed_at TEXT NOT NULL`

历史删除不必强制删除 cache。

### `vocabulary`

- `lemma TEXT PRIMARY KEY COLLATE NOCASE`
- `word_json TEXT NOT NULL`
- `saved_at TEXT NOT NULL`
- `last_viewed_at TEXT NOT NULL`

### `terminology`

- `id TEXT PRIMARY KEY`
- `source TEXT UNIQUE NOT NULL COLLATE NOCASE`
- `target TEXT NOT NULL`
- `created_at TEXT NOT NULL`
- `updated_at TEXT NOT NULL`

### `chats`

- `id TEXT PRIMARY KEY`
- `title TEXT NOT NULL`
- `conversation_context TEXT NOT NULL DEFAULT ''`
- `compact_summary TEXT NOT NULL DEFAULT ''`
- `created_at TEXT NOT NULL`
- `updated_at TEXT NOT NULL`

### `messages`

- `id TEXT PRIMARY KEY`
- `chat_id TEXT NOT NULL REFERENCES chats(id) ON DELETE CASCADE`
- `role TEXT NOT NULL` (`user|assistant|system_internal`)
- `content TEXT NOT NULL`
- `reasoning_content TEXT NOT NULL DEFAULT ''`
- `created_at TEXT NOT NULL`
- `generation_id TEXT NULL`

索引：`(chat_id, created_at)`。

### `open_tabs`

- `id TEXT PRIMARY KEY`
- `kind TEXT NOT NULL` (`word|text`)
- `title TEXT NOT NULL`
- `translation_id TEXT NULL`
- `payload_json TEXT NOT NULL`
- `position INTEGER NOT NULL`
- `is_active INTEGER NOT NULL DEFAULT 0`
- `updated_at TEXT NOT NULL`

## 3. Global Context

可作为 `settings.global_context` 持久化；若未来需要多个 context profile，再迁移到独立表。1.0 不需要过度设计。

## 4. Secrets

API Key **不是数据库字段**。Credential Manager target name 应稳定，例如：

`FloatTranslate/ProviderApiKey`

前端永远只看到 `configured` 与 masked hint。

## 5. Backup JSON

顶层建议：

- `backup_schema_version`
- `exported_at`
- `app_version`
- `settings`
- `translation_history`
- `vocabulary`
- `terminology`
- `chats`
- `messages`
- `open_tabs`

不包含：

- API Key
- Local Session Token
- 临时 generation 状态
- 原生 window secret
- 纯 cache 表（可选排除，以减小备份）

## 6. Import 合并规则

- settings：按备份导入非 secret 设定，冲突以用户确认的导入策略为准；1.0 默认备份值覆盖同 key。
- vocabulary：lemma 去重；较新的 `last_viewed_at` 获胜。
- terminology：source case-insensitive 去重；备份冲突项可覆盖现值。
- history：按 id 去重。
- chats/messages：按 id 去重并保持外键关系。
- tabs：导入后重新规范化 position。

## 7. Clear / Reset

`Clear Local Business Data`：清 history/cache/vocabulary/chats/messages/compact/context；保留 API Key 和基础设置。

`Reset App`：在上述基础上清 settings、Credential Manager API Key、tabs；Tauri 清 window state。
