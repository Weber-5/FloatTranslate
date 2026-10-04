-- FloatTranslate v1 schema (docs/05 §2). Applied as a single transaction by
-- internal/migration. All timestamps are RFC3339 UTC strings.

CREATE TABLE IF NOT EXISTS app_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value_json TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS translation_cache (
    cache_key TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    normalized_input TEXT NOT NULL,
    model TEXT NOT NULL,
    config_hash TEXT NOT NULL,
    result_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_used_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_translation_cache_model_config
    ON translation_cache(model, config_hash);

CREATE TABLE IF NOT EXISTS translation_history (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    input_text TEXT NOT NULL,
    normalized_input TEXT NOT NULL,
    result_json TEXT NOT NULL,
    source TEXT NOT NULL,
    model TEXT NOT NULL,
    created_at TEXT NOT NULL,
    last_viewed_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_translation_history_created_at
    ON translation_history(created_at DESC);

CREATE TABLE IF NOT EXISTS vocabulary (
    lemma TEXT COLLATE NOCASE PRIMARY KEY,
    word_json TEXT NOT NULL,
    saved_at TEXT NOT NULL,
    last_viewed_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS terminology (
    id TEXT PRIMARY KEY,
    source TEXT COLLATE NOCASE NOT NULL UNIQUE,
    target TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS chats (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    conversation_context TEXT NOT NULL DEFAULT '',
    compact_summary TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
    id TEXT PRIMARY KEY,
    chat_id TEXT NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    reasoning_content TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    generation_id TEXT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_chat_created
    ON messages(chat_id, created_at);

CREATE TABLE IF NOT EXISTS open_tabs (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    title TEXT NOT NULL,
    translation_id TEXT NULL,
    payload_json TEXT NOT NULL,
    position INTEGER NOT NULL,
    is_active INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);
