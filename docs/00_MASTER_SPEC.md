# FloatTranslate 1.0 — Master Specification

## 1. 产品定位

FloatTranslate 1.0 是 **Windows 10/11 专用的悬浮英译中阅读工具**。它不试图成为完整词典、OCR 软件或通用知识管理平台；第一版只解决以下高频链路：

1. 复制/输入英文并快速获得中文翻译。
2. 鼠标选中文本后按全局快捷键，自动捕获并翻译。
3. 单词获得结构化词典结果并可收藏。
4. 文本中的英文单词可点击，作为新标签页继续查词。
5. 需要深入解释时，右侧展开独立 AI Sidebar。

## 2. 1.0 冻结范围

### 平台与发布

- 仅 Windows 10/11。
- 单实例运行。
- 安装版：NSIS `.exe`。
- Portable：免安装 `.exe`；业务数据写入程序同目录数据目录。
- 安装版业务数据写入 Windows 用户应用数据目录。
- GitHub Release 发布两个构建产物。
- 代码签名流程预留，但无证书时仍可发布未签名版本。

### 主窗口

- 默认尺寸：420×720 px。
- 最小尺寸：360×520 px。
- 可缩放、可拖动，记忆位置和尺寸。
- 无原生标题栏，顶部完全自绘。
- 默认 Always on Top，可在设置关闭。
- 点击关闭按钮：隐藏到系统托盘，不退出。
- 托盘：显示/隐藏、设置、退出。
- 开机启动：可配置，默认关闭。
- AI Sidebar 展开后默认约 840×720 px。
- 每次启动 Sidebar 默认收起。

### 底部导航

仅 3 项：

1. 翻译
2. 单词本
3. 设置

“翻译历史”位于翻译页面内，不新增底栏入口。

### 标签页

- 浏览器式标签页。
- Word/Text 两种标签页。
- 点击文本中的单词或同义词，新建 Word tab。
- 数量不设上限。
- 1.0 不做 Pin。
- 关闭 Tab 不删除历史。
- 重启恢复上次打开标签页和激活标签页。

## 3. 翻译输入

支持：

- 手动输入。
- 复制粘贴。
- 鼠标划词 + 全局快捷键。

不支持：

- OCR。
- 图片翻译。
- PDF 导入。
- 网页抓取。

手动输入：`Enter` 翻译，`Shift+Enter` 换行；粘贴不会自动翻译。

默认全局快捷键：

- `Ctrl+Alt+Space`：显示/隐藏主窗口。
- `Ctrl+Alt+Q`：快速翻译当前选中文本。

快捷键均可修改，修改后立即重新注册并持久化；冲突必须明确提示。

快速翻译流程：保存用户剪贴板 → 模拟 `Ctrl+C` → 读取选中文本 → 恢复原剪贴板 → 唤醒窗口 → 新建 Tab → 自动发送翻译。

## 4. 翻译模式

系统本地预判 Word / Text。

### Word

结构化字段：

- word
- lemma
- phonetic_uk
- phonetic_us
- parts_of_speech[]
- 每个词性对应多个中文释义
- synonyms[]
- inflections[]

不生成例句。

UK/US 发音调用本地 Windows/WebView2 TTS，不消耗 LLM。

同义词可点击并新建 Word tab。

### Text

- 原文在上，译文在下，按段落对应。
- 保留 Markdown。
- 原文英文 token 可点击；标点和数字不可点击。
- 点击词后本地 lemmatization，再新建 Word tab。
- 中英混合文本允许；若英文不是主要语言，则本地拒绝，并显示“FloatTranslate 1.0 暂仅支持英译中”。
- 代码、URL、LaTeX、路径、数字、技术标识等受保护，不应被模型篡改。
- 长文本按 Markdown 结构、段落和 token budget 分块，再按原顺序重组。

## 5. 单词本

- 收藏完整 Word Structured Result。
- 以 lemma 去重。
- 重复收藏不新增，更新 `last_viewed_at`。
- 搜索。
- 删除。
- 按收藏时间排序。
- 点击卡片新建 Word tab，直接使用本地结构化内容，不重新请求 LLM。

1.0 不做：标签、文件夹、熟练度、背诵系统。

## 6. 翻译缓存与历史

缓存命中条件至少包含：

`normalized input + content kind + translation model + effective translation config hash`

- 命中缓存：不调用 LLM。
- `重新翻译`：绕过缓存，成功后覆盖对应缓存结果。
- API 失败但有缓存：展示缓存并标注“本地缓存”。
- 无缓存：保留输入，页面内显示可重试错误。

历史：

- Word/Text 都记录。
- 可重新打开为新 Tab。
- 单条删除。
- 清空全部。
- 删除历史不删除单词本。
- 关闭 Tab 不删除历史。

## 7. AI Sidebar

- 独立于翻译上下文；默认不自动注入当前翻译。
- 支持多个会话。
- 新建、重命名、删除、停止生成、重新生成最后回答。
- Markdown、代码块、复制。
- 翻译页提供 `Ask AI`，由用户主动把当前内容发给新/指定会话。
- SSE 流式输出。
- 思考按钮默认关闭，仅 Chat 使用；翻译永远关闭思考模式。
- Provider 返回 reasoning 时显示默认折叠的“思考过程”。

### Chat Context 顺序

`System Prompt → Global Context → Conversation Context → Compact Summary → Recent Messages`

- Global Context：全局。
- Conversation Context：单会话。
- 两者均持久化并永不被 compact 改写。
- 默认 Max Context Tokens：1,000,000。
- Max Output Tokens：用户可配置。
- Auto Compact：默认开启。
- Threshold：80%。
- Provider 实际能力不足时按能力限制并在 UI 提示有效值。
- Compact 后原始消息仍保存在 SQLite。

### Slash Commands

输入 `/` 弹出命令菜单，采用可注册架构。

- `/compact`：压缩较早上下文。
- `/clear`：立即永久删除当前会话全部消息与 Compact Summary；保留 Conversation Context；不二次确认。
- `/context`：打开当前 Conversation Context 编辑器。
- `/export`：导出当前会话为 Markdown。

## 8. LLM Provider

- Provider 抽象层。
- 1.0 内置 DeepSeek preset 与 OpenAI-compatible 模式。
- 1.0 只保存一套 Provider 配置。
- Base URL / API Key 共用。
- Translation Model / Chat Model 分开设置。
- 默认模型字符串：`deepseek-flash`，但必须可配置，不能硬编码业务逻辑。
- Provider 失败时不自动 fallback 到其他 Provider。
- Translation 使用 Structured JSON；Go 后端验证 JSON Schema。
- JSON 不合法：自动发起 1 次修复；再次失败返回标准化错误。

## 9. Settings

- Theme：System / Light / Dark，默认 System。
- 1.0 UI 仅简体中文，但所有字符串走 i18n。
- Provider mode / Base URL / API Key / Translation Model / Chat Model / Test Connection。
- API Key 输入可显示/隐藏，但保存后前端不能读取明文。
- Context 输入/输出上限、Auto Compact 和阈值。
- Global Context。
- AI System Prompt：可编辑 + 恢复默认。
- Custom Translation Prompt：高级设置，默认空。
- Terminology：增删改查。
- Always on Top。
- Auto Start。
- Hotkeys。
- Proxy：跟随系统或自定义 HTTP/HTTPS/SOCKS5。
- Check Update：GitHub Releases；只提示和跳转，不静默更新。
- 日志目录 / 清空日志。
- JSON Backup Import/Export。
- Clear Local Business Data：清业务数据，保留 API Key 和基础设置。
- Reset App：二次确认；清数据库、API Key、设置、Context、窗口状态，回到首次启动。

## 10. 首次启动

若无 API Key，必须进入初始化向导，完成：

- API Key
- Base URL
- Translation Model
- Chat Model
- Test Connection

测试成功后进入主界面。

## 11. 数据与隐私

- SQLite 不加密。
- API Key 存 Windows Credential Manager。
- API Key 不写入 SQLite、日志、备份。
- 零遥测。
- 本地日志必须脱敏 Authorization / Bearer / API Key / Session Token。
- 业务数据持续保存直到用户删除。
- JSON Backup 可读，含 `backup_schema_version`，导入采用 merge + deduplicate。

## 12. 1.0 非目标

- macOS/Linux。
- 登录/账户。
- 云同步。
- 多 Provider profile。
- Provider 自动 fallback。
- 多语言翻译。
- OCR/图片/PDF/网页解析。
- 单词例句。
- 背诵系统。
- 标签页 Pin。
- 静默自动更新。
