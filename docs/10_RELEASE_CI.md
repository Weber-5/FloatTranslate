# 10 — CI / Packaging / Release

## 1. Branch & PR

建议：

- `main`：可发布。
- feature branches：PR merge。
- 所有 PR 必过 CI。

## 2. CI Stages

1. Contract：OpenAPI + JSON Schema lint。
2. Frontend：install → lint → typecheck → test → build。
3. Backend：fmt/vet/test/build。
4. Rust：fmt/clippy/test/build（Windows runner）。
5. Integration：sidecar health + API smoke。
6. Package：Tauri/NSIS installer + Portable launcher。

## 3. Release Trigger

建议 git tag：`v1.0.0`。

Release assets：

- `FloatTranslate_1.0.0_x64-setup.exe`
- `FloatTranslate_1.0.0_x64-portable.exe`
- checksum file
- release notes

## 4. Code Signing

CI 必须预留条件式签名步骤：

- 未配置 signing secrets：正常产出 unsigned build。
- 已配置：对 installer/portable 签名。
- secrets 永不写入 repo。

## 5. Update Check

应用读取 GitHub Releases 最新稳定版本：

- 比较 semantic version。
- 若有新版本，展示版本号/release notes link。
- 用户主动点击跳转下载。
- 1.0 不静默下载、不静默安装。

## 6. Release Notes 模板

- Highlights
- Fixes
- Known Issues
- Privacy/Security changes
- Community contributors

## 7. Versioning

App / Go backend / OpenAPI / Backup Schema / DB Schema 分别有版本，但应用发布统一采用 SemVer。
