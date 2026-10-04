# FloatTranslate ${VERSION} (${TAG_NAME})

## Highlights

- 首个稳定版本：快速划词/粘贴翻译、结构化 LLM 词典、单词本、多标签页阅读与 AI Sidebar。
- 本地优先：业务后端以 sidecar 形式运行在本机（127.0.0.1 + 一次性会话令牌），数据存于本地 SQLite。
- 安装版（NSIS，当前用户安装）与 Portable 版双发布。

## Fixes

- 首个稳定版本，无已修复缺陷列表。

## Known Issues

- Portable 版为单文件发布；如需便携数据目录，请在 exe 旁放置 `portable.flag`（数据将写入同目录 `FloatTranslateData/`）。
- 仅内置 DeepSeek preset；OpenAI-compatible Provider 适配欢迎社区贡献。

## Privacy / Security changes

- 零遥测；API Key 仅存 Windows Credential Manager，日志脱敏。
- 后端仅绑定 127.0.0.1 动态端口，并要求每次运行生成的本地会话 Bearer Token。

## Community contributors

- 感谢所有参与需求评审、测试与文档贡献的社区成员。
