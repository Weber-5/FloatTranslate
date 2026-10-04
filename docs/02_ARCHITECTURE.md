# 02 — Architecture

## 1. 总体架构

```text
┌──────────────────────────────┐
│ Vue 3 + TypeScript           │
│ Presentation / View State    │
└──────────────┬───────────────┘
               │ REST / SSE
               │ Authorization: Bearer <session token>
               ▼
┌──────────────────────────────┐        HTTPS        ┌──────────────────────┐
│ Go Backend Sidecar           │────────────────────►│ LLM Endpoint         │
│ 127.0.0.1:<dynamic-port>     │                     │ DeepSeek/OpenAI-like │
│ Domain / Application / Data  │                     └──────────────────────┘
└──────────────┬───────────────┘
               │
               ├── SQLite
               └── Windows Credential Manager

┌──────────────────────────────────────────────────────────────┐
│ Rust / Tauri 2 Host                                          │
│ Window · Tray · Hotkeys · Clipboard · File Dialog            │
│ Single Instance · Autostart · Sidecar Supervisor             │
└──────────────────────────────────────────────────────────────┘
```

## 2. 强制边界

### Vue

允许：
- UI、路由、Pinia、Tab View State、表单、Markdown 渲染、SSE 展示。

禁止：
- 直接打开 SQLite。
- 直接请求 LLM。
- 持久化 API Key 明文。
- 实现业务级 cache/compact/translation logic。

### Go

负责：
- REST/SSE。
- Translation / Dictionary / Vocabulary / History。
- Chat / Context / Compact / Commands。
- Provider abstraction。
- JSON Schema validation + repair。
- NLP / language detection / lemmatization。
- Terminology / Protected Span。
- SQLite / Migration / Backup。
- Credential Manager。
- Logging / redaction。
- Proxy / provider connection。

### Rust/Tauri

负责：
- Window / frameless / always-on-top。
- Tray。
- Global Hotkeys。
- Clipboard + selection capture。
- File dialog。
- Single instance。
- Autostart。
- Sidecar start/supervise/restart。
- Window geometry persistence。

禁止：
- Translation/Chat domain logic。
- SQLite business access。
- LLM calls。

## 3. Go 模块建议

```text
cmd/server
internal/api
internal/translation
internal/dictionary
internal/vocabulary
internal/history
internal/chat
internal/context
internal/command
internal/llm
internal/llm/deepseek
internal/llm/openai_compatible
internal/nlp
internal/terminology
internal/protectedspan
internal/cache
internal/repository
internal/database
internal/migration
internal/credential
internal/backup
internal/config
internal/logging
internal/proxy
```

模块只能通过明确接口依赖；禁止跨模块直接操作对方 repository。

## 4. Rust/Tauri 模块建议

```text
window
tray
hotkey
clipboard
selection
file_dialog
single_instance
autostart
sidecar
app_lifecycle
window_state
```

## 5. 启动序列

1. Tauri 获得单实例锁；若已有实例，唤醒旧窗口并退出新实例。
2. 决定 Data Root：Installed 或 Portable。
3. 生成高熵随机 Local Session Token。
4. 启动 Go Sidecar：只允许 loopback，动态端口。
5. Go 打开 SQLite，执行 migration。
6. Go 初始化 credential/provider/config。
7. Go 监听端口并返回 READY + port。
8. Tauri 将 backend base URL + session token 暴露给当前 WebView 会话。
9. Vue 初始化 API client。
10. 若 credential 不完整 → Onboarding；否则进入主 UI。

## 6. Sidecar 恢复

- Rust 监测 Go 进程异常退出。
- 使用有限次数、带退避的自动重启。
- 每次进程重启可复用 data root，但运行时连接状态重新建立。
- Vue 显示 backend restarting 状态。
- 连续失败后停止重启并展示可恢复错误和日志入口。

## 7. 请求数据流

### Translation

`Vue → POST /translate → Go validation → NLP → cache → provider → schema validate/repair → persist → DTO → Vue`

### Chat

`Vue → streaming POST → Go compose context → provider stream → SSE → Vue`

### Selection Hotkey

`Windows App → Ctrl+Alt+Q → Rust selection capture → Vue route/new tab → Go /translate`

## 8. 依赖方向

应用层依赖 domain interface；infrastructure 实现 interface。建议依赖方向：

`API → Application Service → Domain Interface ← Infrastructure Adapter`

避免把 HTTP、SQLite、DeepSeek vendor 字段泄漏进核心业务模型。
