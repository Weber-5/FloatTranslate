# 01 — Product Requirements Document

## 1. 目标用户与核心场景

目标用户是在 Windows 上高频阅读英文网页、论文、PDF 阅读器内容、IDE 文档或聊天内容的人。FloatTranslate 不接管阅读器，而是作为始终可快速唤起的小窗口存在。

### 核心用户故事

**US-01 快速查词**  
用户粘贴 `suspended`，系统识别为 Word，显示 UK/US IPA、词性、中文释义、同义词和词形变化；用户可收藏。

**US-02 文本翻译**  
用户粘贴一段英文，按 Enter，系统保留 Markdown，将英文和中文按段落上下排列。

**US-03 点击词继续查**  
用户在文本翻译页点击 `running`，系统本地还原为 `run`，新建 Word tab，不破坏原文本页。

**US-04 划词即译**  
用户在浏览器/Zotero/Word/PDF 阅读器等程序选中文字，按 `Ctrl+Alt+Q`，软件自动复制、恢复剪贴板、唤醒、建 Tab、翻译。

**US-05 单词收藏**  
用户点击收藏；以后在单词本打开时不再次调用 LLM。

**US-06 Ask AI**  
用户在翻译页主动点击 Ask AI，把内容发入右侧 AI Sidebar，继续解释语法、概念等。

**US-07 长会话**  
Chat 上下文到达配置阈值后自动 compact；用户也可以 `/compact`。

## 2. 功能优先级

### P0 — 必须完成

- 主窗口/托盘/置顶/快捷键/单实例。
- Word/Text 翻译。
- Structured Output 校验与一次修复。
- 缓存与历史。
- 多 Tab 恢复。
- 单词本。
- AI Sidebar + SSE。
- Global/Conversation Context + Compact。
- Settings + Credential Manager。
- SQLite Migration。
- NSIS + Portable。

### P1 — 1.0 质量能力

- Protected Span。
- Terminology。
- Custom Translation Prompt。
- Editable AI System Prompt。
- Backup Import/Export。
- Proxy。
- Update Check。
- 日志脱敏。

## 3. 关键状态机

### Translation Tab

`empty → input_ready → translating → success | cache_success | retryable_error`

重新翻译从 `success/cache_success` 进入 `translating`，成功后替换当前结果。

### Chat Generation

`idle → preparing_context → streaming → completed | cancelled | error`

禁止同一会话同时存在多个活动 generation。

### Backend Health

`starting → ready → restarting → ready | failed`

Rust 负责后端重启，Vue 只消费状态。

## 4. 性能目标

这些是工程验收目标，不是对第三方 LLM 网络延迟的保证：

- 空闲内存占用应明显低于典型 Electron 同类工具；具体阈值在实现阶段基准化。
- 热键触发到窗口显示：目标 < 300ms。
- 本地缓存命中到内容可见：目标 < 150ms。
- UI 操作保持 60fps 级流畅，不因日志/SQLite/网络请求阻塞主线程。
- SQLite 查询使用索引，单词本和历史在 10k 级记录下仍应即时响应。

## 5. 可用性要求

- 网络/API 错误均以内联状态展示，不弹系统错误框。
- API Key 未配置不得进入不可理解的报错循环；首次启动必须先测试连接。
- 任何 destructive 操作必须明确标识；例外：用户已明确要求 `/clear` 不二次确认。
- 任何 Provider 能力不匹配都应告诉用户“配置值”和“实际生效值”。

## 6. 成功标准

1. 用户能从 GitHub 下载 `.exe` 并安装/运行。
2. 初次设置后 3 次交互内完成第一次翻译。
3. 划词快捷键在 Chrome/Edge/Word/Zotero 等常见应用中具备高兼容性。
4. 关闭主窗口不会结束服务，托盘可恢复。
5. 重启后恢复 Tabs 和数据。
6. 断网时已有缓存仍能工作。
7. 所有关键业务模块有自动化测试。
