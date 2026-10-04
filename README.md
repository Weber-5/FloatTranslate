# FloatTranslate 1.0 — 完整开发文档

> 状态：**需求冻结 / Ready for implementation**  
> 平台：Windows 10/11  
> License：MIT  
> 文档版本：1.0.0

FloatTranslate 是一款面向日常外文阅读的 Windows 悬浮英译中工具。1.0 聚焦“快速划词/粘贴翻译 + LLM 结构化词典 + 单词本 + 多标签页 + AI Sidebar”，坚持本地优先、无遥测、OpenAPI 契约优先和模块化并行开发。

## 技术栈

- UI：Vue 3 + TypeScript + Vite + Pinia + Vue Router
- Windows Host：Rust + Tauri 2
- 业务后端：Go
- 数据库：SQLite
- 前后端契约：OpenAPI 3.1
- Chat 流式协议：SSE
- LLM：DeepSeek preset + OpenAI-compatible Provider abstraction
- 安装：NSIS `.exe` + Portable `.exe`

## 强制架构边界

1. **Vue 只负责 UI 与 View State，不直接访问 SQLite，不直接调用 LLM。**
2. **Go 是业务层和 SQLite 的唯一所有者。**
3. **Rust/Tauri 只负责 Windows 原生能力与 Go Sidecar 生命周期。**
4. `openapi/openapi.yaml` 是前后端唯一 API 契约源。
5. API Key 由 Go 写入 Windows Credential Manager；前端只能写，不能读回明文。
6. Go REST 服务仅绑定 `127.0.0.1` 动态端口，并要求每次运行生成的 Local Session Bearer Token。

## 文档目录

| 文档 | 用途 |
|---|---|
| `docs/00_MASTER_SPEC.md` | 1.0 冻结范围与全局约束 |
| `docs/01_PRD.md` | 产品需求与用户流程 |
| `docs/02_ARCHITECTURE.md` | 总体架构、模块职责、启动流程 |
| `docs/03_UI_UX_SPEC.md` | Apple 极简风 UI/交互规范 |
| `docs/04_API_CONTRACT.md` | REST/SSE 接口设计与错误模型 |
| `docs/05_DATA_MODEL.md` | SQLite 数据模型、迁移、备份 |
| `docs/06_LLM_PROTOCOL.md` | Provider、翻译 Schema、Chat/Compact |
| `docs/07_WINDOWS_HOST_SPEC.md` | Tauri/Rust 原生能力规范 |
| `docs/08_SECURITY_PRIVACY.md` | 安全与隐私边界 |
| `docs/09_TEST_ACCEPTANCE.md` | 测试矩阵和验收标准 |
| `docs/10_RELEASE_CI.md` | CI/CD、打包、Release、签名预留 |
| `docs/11_AGENT_DEVELOPMENT.md` | 多 Agent 并行开发与合并顺序 |
| `docs/12_CONTRIBUTING.md` | GitHub 社区贡献规则 |
| `docs/13_DECISIONS.md` | 已冻结技术决策与 1.0 非目标 |
| `openapi/openapi.yaml` | 机器可读 API 契约 |
| `schemas/*.schema.json` | LLM/备份 JSON Schema |

## 1.0 Roadmap / TODO / Community Tasks

状态：`FROZEN` 需求冻结，`DONE` 已实现并通过测试，`PLANNED` 待实现，`COMMUNITY` 适合社区贡献。

| 状态 | 模块 | 任务 |
|---|---|---|
| FROZEN | Architecture | Vue + Go + Rust/Tauri + SQLite + OpenAPI/SSE 架构 |
| FROZEN | Translation | 单词/文本双模式、结构化输出、缓存、重翻译 |
| FROZEN | Reading UX | 多标签页、历史、点击单词/同义词跳转 |
| FROZEN | Vocabulary | 收藏、去重、搜索、删除、按收藏时间排序 |
| FROZEN | AI Sidebar | 多会话、思考按钮、SSE、compact、slash commands |
| FROZEN | Context | Global + Conversation Context；1M 默认；80% auto compact |
| FROZEN | Windows | 置顶、托盘、热键、划词、单实例、Sidecar 监督 |
| FROZEN | Privacy | 零遥测、Credential Manager、日志脱敏 |
| DONE | Frontend | Vue 组件和 Design System（vitest 162 用例全绿） |
| DONE | Backend | Go REST/SSE 业务实现（翻译/Chat/Context/Compact/Backup） |
| DONE | Windows Host | Tauri/Rust 原生能力实现（窗口/托盘/热键/划词/单实例） |
| DONE | Release | NSIS + Portable 本地构建验证；CI/Release 工作流就绪 |
| COMMUNITY | i18n | English / 日本語语言包 |
| COMMUNITY | Providers | OpenAI / Gemini / Claude / Ollama / vLLM adapters |
| COMMUNITY | Vocabulary | 标签、文件夹、熟练度、复习系统 |
| COMMUNITY | Input | OCR、PDF、网页解析（不属于 1.0） |

## 1.0 交付物定义

1. NSIS 安装版 `.exe`。
2. Portable `.exe`。
3. GitHub Release。
4. README / LICENSE / CONTRIBUTING。
5. OpenAPI、Schema、CI、测试报告。
6. 不包含账户系统、云同步、OCR/PDF/网页抓取、多语种翻译。
