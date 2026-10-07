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

### 5.1 应用内自动更新（1.1.1）

安装版在发现新版本时提供**「立即更新」**按钮，点击后：

1. `tauri-plugin-updater` 读取
   `https://github.com/Weber-5/FloatTranslate/releases/latest/download/latest.json`；
2. 校验 minisign 签名（公钥在 `tauri.conf.json` 的 `plugins.updater.pubkey`）后下载更新包；
3. 静默运行 NSIS 更新并调用 `process.relaunch()` 重启，用户看到的是「正在下载…%」与重启。

仍然**不静默**：只有用户点击才会下载安装；失败/无签名包时给出明确提示并保留手动下载链接。

发布侧要求（`.github/workflows/release.yml`）：

- 构建时注入 `TAURI_SIGNING_PRIVATE_KEY` / `TAURI_SIGNING_PRIVATE_KEY_PASSWORD`（仓库 secrets），
  否则 bundler 不产出签名更新包；
- 产物需带上 `FloatTranslate_<ver>_x64-setup.exe.sig`（新版 bundler 直接对安装 exe 签名；
  旧版为 `*-setup.nsis.zip` + `.sig`），并上传 `latest.json`；
- `createUpdaterArtifacts: true` 已在 `tauri.conf.json` 中开启。

免安装版（portable）无法自更新：UI 显示手动替换提示，`get_backend_config` 额外返回
`portable: true` 供前端判断。

## 6. Release Notes 模板

- Highlights
- Fixes
- Known Issues
- Privacy/Security changes
- Community contributors

## 7. Versioning

App / Go backend / OpenAPI / Backup Schema / DB Schema 分别有版本，但应用发布统一采用 SemVer。
