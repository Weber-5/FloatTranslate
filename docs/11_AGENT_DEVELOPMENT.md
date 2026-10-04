# 11 — Multi-Agent Development Plan

## 1. 原则

先契约、后并行。任何子代理不得“边写边发明接口”。

## 2. Agent 角色

### Agent 0 — Architecture / Contract Owner

唯一可在协调后修改：

- OpenAPI
- JSON Schemas
- Cross-layer DTO names
- 数据迁移设计
- Architecture decisions

职责：解决跨 Agent 冲突。

### Agent 1 — Frontend

Scope：Vue 3 + TypeScript。

负责：
- Design System。
- Tabs/Translate/Vocabulary/Settings/History。
- AI Sidebar。
- SSE renderer。
- i18n。
- 前端测试。

禁止：SQLite/LLM direct call。

### Agent 2 — Go Backend

负责：
- REST/SSE。
- Translation/Chat/Context/Compact。
- Provider。
- NLP/Terminology/Protected Span。
- SQLite/Migration/Backup。
- Credential Manager。
- tests。

### Agent 3 — Rust/Tauri Host

负责：
- Windows native integration。
- Window/tray/hotkey/clipboard/selection。
- single-instance/autostart。
- sidecar lifecycle。
- file dialog。

禁止：业务 DB / LLM logic。

### Agent 4 — Integration / Release

负责：
- CI。
- contract checks。
- integration tests。
- installer/portable。
- signing hooks。
- release automation。

## 3. 并行顺序

### Phase 0 — Contract Freeze

必须先完成：

- `openapi/openapi.yaml`
- schemas
- data model
- error model
- SSE events

### Phase 1 — Skeleton in parallel

- Frontend：mock API client + static screens。
- Backend：health/settings/mock translation。
- Tauri：window + sidecar health。

### Phase 2 — Vertical Slice

完成一条可工作的链路：

`input → Go → provider mock/real → word result → UI`

### Phase 3 — Reading Core

- text translation
- tabs
- history
- vocabulary
- hotkey selection

### Phase 4 — AI

- chats
- SSE
- thinking
- context
- compact
- slash commands

### Phase 5 — Hardening

- credentials
- proxy
- backup
- migrations
- logs/redaction
- reset

### Phase 6 — Release

- installer
- portable
- update check
- CI gates

## 4. API Change Protocol

任何 Agent 发现需要 API 改动：

1. 提交 contract change proposal。
2. Agent 0 修改/批准 OpenAPI。
3. 生成/同步 client/server types。
4. 前后端各自实现。
5. CI contract compatibility pass。

禁止只改实现不改 OpenAPI。

## 5. Definition of Done

每项功能只有同时满足以下条件才算完成：

- 功能实现。
- 自动化测试。
- OpenAPI/Schema 同步。
- 错误/空状态实现。
- 日志无敏感信息。
- README Roadmap 状态更新。
- Acceptance criteria 通过。

## 6. Agent Prompt 模板

给每个 Agent 的任务必须包含：

- Scope directory。
- Allowed dependencies。
- Forbidden responsibilities。
- Relevant OpenAPI endpoints。
- Relevant acceptance tests。
- Do not change contract without approval。

这样可显著降低多代理互相踩文件和接口漂移。
