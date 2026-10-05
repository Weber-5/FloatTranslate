; FloatTranslate NSIS installer hooks (Tauri v2 bundle.windows.nsis.installerHooks).
; improvement bug #3: after install/upgrade, Windows Explorer can keep showing
; the OLD (or blank) icon for the exe because of the shell icon cache. The
; standard refresh is `ie4uinit -show`, which flushes the per-user icon cache
; without a reboot or explorer restart.

!macro NSIS_HOOK_POSTINSTALL
  nsExec::ExecToLog 'ie4uinit.exe -show'
!macroend
