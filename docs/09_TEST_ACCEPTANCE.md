# 09 — Test & Acceptance

## 1. PR 必过 Quality Gate

- Go unit/integration tests。
- Rust tests。
- Vue lint。
- Vue typecheck。
- Vue component/unit tests。
- OpenAPI validation。
- JSON Schema validation tests。
- Windows build check。

## 2. Go 核心测试

### Translation

- Word/Text 分类。
- Unsupported language。
- cache hit/miss。
- bypass cache。
- schema success。
- first invalid → repair success。
- second invalid → error。
- provider fail + cache fallback。
- provider fail + no cache。
- terminology hard constraint pre/post check。
- protected span restore。

### NLP

至少覆盖：
- running → run
- studies → study
- punctuation separation
- URL/code/math 不作为普通 token

### Repository

- vocabulary dedupe。
- history delete 不影响 vocabulary。
- chat cascade delete。
- migration from previous schema fixture。
- backup merge/dedupe。

### Context

- 1M configured。
- 80% auto compact。
- provider capability clamp。
- compact 不改 Global/Conversation Context。
- raw messages retained。
- `/clear` 删除 messages + summary，保留 Conversation Context。

## 3. Rust/Tauri 测试

- single instance。
- show/hide hotkey。
- hotkey conflict rollback。
- clipboard restore。
- sidecar restart。
- close-to-tray。
- app exit sidecar cleanup。
- installed/portable data root resolution。

## 4. Frontend 测试

- tab create/close/restore。
- clicking token opens word tab。
- synonym opens word tab。
- loading/error/cache states。
- API Key not visible after save。
- SSE content/reasoning rendering。
- Stop/Regenerate。
- slash command picker。
- theme/i18n strings。

## 5. E2E 关键路径

### E2E-01 First Run
Install → onboarding → save key → test connection → main UI → word translate。

### E2E-02 Selection Translation
Browser select English → hotkey → app appears → new tab → translation → clipboard unchanged。

### E2E-03 Text Drilldown
Translate paragraph → click `studies` → new tab `study` → back to original tab intact。

### E2E-04 Vocabulary
Save word → restart → vocabulary exists → open without provider request。

### E2E-05 Offline Cache
Translate online → disconnect → same request → local cache shown。

### E2E-06 Chat
New chat → stream → enable thinking → reasoning collapsed → `/compact` → restart → history retained。

### E2E-07 Reset
Reset App → second confirmation → restart → onboarding, key removed。

## 6. 1.0 Release Acceptance Checklist

- [ ] Windows 10/11 smoke test。
- [ ] Installer installs/uninstalls cleanly。
- [ ] Portable does not write business DB into AppData。
- [ ] API Key never appears in SQLite/backup/logs。
- [ ] OpenAPI matches server/client。
- [ ] No telemetry endpoint。
- [ ] README Roadmap up to date。
