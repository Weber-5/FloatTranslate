# 03 — UI / UX Specification

## 1. 视觉方向

“Apple 极简风格”定义为视觉原则，而不是像素级仿制：

- 大量留白。
- 轻量材质层次。
- 细分隔线。
- 12–18 px 圆角。
- 克制阴影。
- 动画短、轻、可预测。
- 避免后台管理系统式重边框和密集卡片。
- 支持 System/Light/Dark。

## 2. 主窗口布局

### Header

包含：
- 可拖动区域。
- Browser-like Tabs。
- 新建/关闭 Tab。
- History 按钮。
- AI Sidebar toggle。
- 最小化。
- 关闭（隐藏到托盘）。

### Body

由当前底部导航和当前 Tab 决定。

### Bottom Navigation

固定三项：翻译 / 单词本 / 设置。

## 3. Translation — Empty/Input

- 输入区域优先可见。
- Placeholder 用简洁中文。
- Enter 发送，Shift+Enter 换行。
- Send button 始终提供。
- 粘贴只填充，不自动请求。

## 4. Word Result

视觉层级：

1. `word` 大字号 + 收藏按钮。
2. UK / US IPA + 两个 speaker action。
3. Inflections，弱化为辅助信息。
4. Part of speech group：词性标签 + 中文释义列表。
5. Synonym chips：可点击。
6. 状态条：模型/缓存/重新翻译/Ask AI。

无例句区域。

## 5. Text Result

每一逻辑段落：

- 上：英文原文。
- 下：中文译文。
- 段落之间明显留白。

英文 token 视觉上保持“正常文章”而不是满屏按钮；Hover/Focus 时才体现可点击。

Protected spans 维持 code/math 的原排版。

## 6. Tabs

- Word/Text 可混合。
- 新点击单词总是新建 Tab。
- 可关闭。
- 无限数量，但 Tab bar 需要横向滚动或压缩策略。
- 1.0 无 Pin。
- 重启恢复。

## 7. Vocabulary

卡片展示：word、IPA（可选摘要）、主释义、收藏日期。

顶部：搜索。  
排序：默认按收藏时间倒序。  
操作：打开、删除。

## 8. History

从 Translate Header 打开抽屉/页面层，不新增底栏。

内容：类型、原文摘要、时间、来源（cache/model）。

操作：打开、单删、清空全部。

## 9. AI Sidebar

右侧展开；默认宽度与主窗内容区约相同。

组成：

- 会话标题/会话切换。
- 消息区。
- reasoning 折叠区。
- Composer。
- Thinking toggle。
- Slash command picker。
- Send / Stop。

`/` 在 composer 中触发命令菜单。

## 10. Settings 信息架构

建议分组：

1. General：Theme / Always on Top / Auto Start / Hotkeys。
2. Provider：Mode / Base URL / API Key / Translation Model / Chat Model / Test。
3. AI Context：Context Tokens / Output Tokens / Auto Compact / Threshold / Global Context / System Prompt。
4. Translation：Terminology / Custom Translation Prompt。
5. Network：System / HTTP / HTTPS / SOCKS5 proxy。
6. Data：Backup / Import / Clear Local Data / Reset App。
7. About：Version / Check Update / Logs / GitHub。

## 11. 状态设计

每个异步视图必须有：

- idle
- loading
- success
- empty
- cache
- retryable error
- fatal/unavailable（仅必要时）

所有业务错误优先 inline toast/banner，不使用阻塞 OS modal。

## 12. Accessibility

- 所有 icon button 有 tooltip/aria-label。
- 键盘可遍历主要控件。
- 明暗主题对比度达常规可读标准。
- Focus ring 不应被设计隐藏。
