# 12 — Contributing

感谢参与 FloatTranslate。

## 强制规则

1. OpenAPI 是业务接口单一真相源。
2. Go 是 SQLite 唯一 owner。
3. Rust/Tauri 不实现翻译/Chat 业务。
4. Vue 不直接请求 LLM。
5. API Key 不得进入源码、SQLite、日志、备份。
6. 新业务模块必须有测试。
7. UI 文案必须通过 i18n key，不硬编码未来难以翻译的散落文本。

## Pull Request Checklist

- [ ] API change 已先更新 OpenAPI。
- [ ] Schema change 已提供 migration 设计。
- [ ] Go tests pass。
- [ ] Rust tests pass。
- [ ] Vue lint/typecheck/test pass。
- [ ] OpenAPI/JSON Schema lint pass。
- [ ] 无 credential/token 泄露。
- [ ] README Roadmap/TODO 已同步。

## Community-friendly areas

- i18n language packs。
- Provider adapters。
- UI accessibility。
- NLP accuracy improvements。
- Documentation/tests。

## 1.0 不接受的 scope creep

除非 Maintainer 明确批准，1.0 阶段不加入 OCR、PDF parsing、云同步、账号体系、多语种翻译和学习系统。
