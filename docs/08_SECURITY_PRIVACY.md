# 08 — Security & Privacy

## 1. Threat Model

主要风险：

- 本机其他进程调用 localhost API。
- API Key 泄露到数据库/日志/UI。
- 恶意或异常 LLM 输出污染前端。
- 自定义 Base URL 把内容发送到用户未意识到的第三方。
- Backup 意外包含 secret。

## 2. Local API

- Release build 禁止绑定 `0.0.0.0`。
- 仅 `127.0.0.1`。
- 动态端口。
- 每次应用运行生成随机 bearer token。
- 除 health 外所有业务 endpoint 必须验证 token。
- CORS 仅允许预期 Tauri origin / 明确的 dev origin。

## 3. Credential

- API Key 使用 Windows Credential Manager。
- UI write-only。
- GET settings 不返回明文。
- 修改 Key 必须重新提交完整值。
- Reset App 删除 credential。

## 4. Logging Redaction

必须统一过滤：

- `Authorization`
- `Bearer *`
- `api_key`
- Local Session Token
- Credential Manager secret

Debug 日志同样适用。

默认不记录完整用户翻译内容；若调试需要内容级日志，应仅在开发构建显式开启，Release 禁止。

## 5. LLM Data Boundary

设置页明确展示当前 Base URL。只有用户主动发起的翻译/Chat 内容发送到该 endpoint。

零遥测：项目自身不上传 usage/analytics。

## 6. Structured Output Safety

- Translation 结果先 schema validate。
- Markdown renderer 禁止不安全 HTML 或使用严格 sanitize。
- 链接点击走安全外部打开策略。

## 7. Backup

Backup 永不包含：API Key、session token、Authorization、raw credential metadata。

## 8. Proxy

自定义 proxy 视为用户显式网络配置；设置页应清楚显示当前模式。

## 9. Security Acceptance

- grep/static scan 不应在 release bundle/config 中发现测试 API Key。
- 日志测试包含 secret marker 时输出必须被 redact。
- 非 loopback 访问无法建立服务连接。
