# 07 — Windows Host / Tauri Specification

## 1. 窗口

- Frameless。
- 默认 420×720。
- min 360×520。
- resizable。
- default always-on-top。
- remember geometry。
- close → hide to tray。
- minimize → 标准最小化行为。
- Sidebar 展开调整总宽度；关闭恢复主窗宽度。

## 2. Tray

至少：

- 显示/隐藏。
- 设置。
- 退出。

真正退出必须终止 Sidecar 并释放资源。

## 3. Single Instance

重复启动：

1. 新实例检测到已有实例。
2. 通知旧实例 show/focus。
3. 新实例退出。

## 4. Global Hotkeys

默认：

- `Ctrl+Alt+Space` show/hide。
- `Ctrl+Alt+Q` translate selection。

用户修改后：

- 先验证格式。
- 尝试注册新快捷键。
- 成功后再替换旧注册并持久化。
- 失败不丢失旧快捷键，UI 提示冲突。

## 5. Selection Capture

算法：

1. 记录当前前台窗口。
2. 读取并暂存剪贴板中可恢复的常见格式，至少保证纯文本不丢失。
3. 模拟 Ctrl+C。
4. 等待剪贴板更新（短超时 + 轮询/事件）。
5. 获取文本。
6. 恢复原剪贴板。
7. 唤醒 FloatTranslate。
8. 向前端发送 `selection-captured(text)`。
9. 前端新建 Tab 并自动调用 Translation API。

失败时不应破坏原剪贴板。

## 6. File Dialog

Tauri 只负责原生文件选择：

- Backup export destination。
- Backup import source。
- Chat Markdown export destination。

实际序列化/校验由 Go。

## 7. Sidecar Lifecycle

- 动态端口。
- loopback only。
- Rust 生成 Local Session Token。
- 读取 READY handshake。
- unexpected exit 自动有限重启。
- app exit graceful shutdown，超时后 terminate。

## 8. Installed vs Portable

### Installed

数据根目录：Windows AppData 下 FloatTranslate 专用目录。

### Portable

数据根目录：Portable `.exe` 同目录下 `FloatTranslateData/`。

API Key 无论哪种模式都留在 Windows Credential Manager，因此复制 Portable 到新电脑后需要重新配置 Key。

## 9. Autostart

设置开关，默认 off。必须只在用户明确打开后注册。
