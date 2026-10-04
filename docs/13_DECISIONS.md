# 13 — Frozen Architecture Decisions

## ADR-001 Windows only

1.0 只做 Windows 10/11，以保证全局快捷键、划词、托盘、Credential Manager 和窗口行为稳定。

## ADR-002 Tauri + Vue, not Electron

目标是轻量悬浮工具；Tauri 提供更小的运行时边界，Vue 保持前端开发效率。

## ADR-003 Go for business backend

LLM/REST/SSE/JSON/SQLite/并发是 Go 的优势域，且更利于开源社区参与；Rust 保持为窄系统层。

## ADR-004 REST + SSE

普通业务 REST；Chat 单向流式 SSE。1.0 无需 WebSocket 的双向复杂度。

## ADR-005 Go owns SQLite

避免 Go/Rust 双写数据库导致锁、migration 和 domain ownership 混乱。

## ADR-006 Credential Manager owned by Go

调用 LLM 的是 Go，因此 secret owner 也应是 Go，避免额外 Rust→Go secret IPC。

## ADR-007 Localhost token auth

本机 loopback 也不等于可信；使用每次运行随机 bearer token。

## ADR-008 LLM-generated dictionary

1.0 不依赖第三方词典 API；单词数据全部由 LLM structured output 生成并校验。

## ADR-009 Local NLP

Tokenization / language detection / lemmatization 尽量本地完成，减少 API 成本和延迟。

## ADR-010 No DB encryption

1.0 使用普通 SQLite；secret 与业务数据分离。降低部署和 migration 复杂度。

## ADR-011 Portable is data-portable, not secret-portable

Portable 的 SQLite/日志在 exe 同目录；API Key 仍留在当前 Windows Credential Manager。

## ADR-012 No telemetry

1.0 零遥测；只保留本地日志。
