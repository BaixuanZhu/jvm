; jvm - NSIS installer for Windows
;
; Usage:
;   makensis /DAPP_VERSION=0.1.0 [/DAPP_ARCH=arm64] installer/jvm.nsi
;
; Output:
;   dist/jvm-windows-<arch>-setup.exe
;
; Design:
;   - Per-user install to %LOCALAPPDATA%\Programs\jvm (no UAC, no admin)
;   - Environment config (PATH + shell profile) is delegated to jvm.exe itself,
;     which already self-configures on first run via env.EnsureUserPath and
;     shell.EnsureIntegration. The installer just drops the exe and runs it once.
;   - The installer stub itself is x86; on ARM64 Windows it runs under the
;     built-in x86 emulation, while the jvm.exe it drops is the native
;     ${APP_ARCH} build.
;
; Path note: NSIS resolves File paths relative to this script's directory
; (installer/), so the built exe is referenced as "..\dist\<arch>\jvm.exe"
; (the Makefile builds into per-arch subdirectories).

Unicode true
ManifestDPIAware true

; Version injected from the command line: /DAPP_VERSION=x.y.z
!ifndef APP_VERSION
  !define APP_VERSION "0.0.0"
!endif

; Target CPU arch injected from the command line: /DAPP_ARCH=amd64|arm64
!ifndef APP_ARCH
  !define APP_ARCH "amd64"
!endif

Name "jvm ${APP_VERSION}"
OutFile "..\dist\jvm-windows-${APP_ARCH}-setup.exe"
InstallDir "$LOCALAPPDATA\Programs\jvm"
; Reuse previous install dir on upgrade
InstallDirRegKey HKCU "Software\jvm" "InstallDir"
RequestExecutionLevel user
ShowInstDetails show
ShowUnInstDetails show
BrandingText "jvm ${APP_VERSION}"

; ---------- MUI2 (modern UI) ----------
!include "MUI2.nsh"
!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_NOAUTOCLOSE
!define MUI_UNFINISHPAGE_NOAUTOCLOSE

; Installer pages
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

; Uninstaller pages
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

; Languages (English first = default)
!insertmacro MUI_LANGUAGE "English"

; ---------- Section: core files (required) ----------
Section "jvm executable (required)" SecCore
  SectionIn RO
  SetOutPath "$INSTDIR"
  File "..\dist\${APP_ARCH}\jvm.exe"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  ; Remember install location (reused on upgrade)
  WriteRegStr HKCU "Software\jvm" "InstallDir" "$INSTDIR"

  ; Add/Remove Programs entry (per-user, HKCU)
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\jvm" "DisplayName" "jvm - Java version manager"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\jvm" "DisplayVersion" "${APP_VERSION}"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\jvm" "Publisher" "BaixuanZhu"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\jvm" "URLInfoAbout" "https://github.com/BaixuanZhu/jvm"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\jvm" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\jvm" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\jvm" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\jvm" "DisplayIcon" '"$INSTDIR\jvm.exe"'
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\jvm" "NoModify" 1
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\jvm" "NoRepair" 1
SectionEnd

; ---------- Section: configure PATH and shell integration (recommended) ----------
Section "Configure PATH and shell integration (recommended)" SecConfig
  ; Run jvm.exe once to trigger its built-in self-configuration:
  ;   - appends $INSTDIR to the user PATH (registry HKCU\Environment)
  ;   - injects PowerShell $PROFILE and ~/.bashrc integration functions
  ; nsExec avoids a flashing console window.
  nsExec::ExecToLog '"$INSTDIR\jvm.exe" version'
SectionEnd

; ---------- Section: Start Menu shortcut to uninstaller ----------
Section "Start Menu uninstall shortcut" SecShortcut
  CreateDirectory "$SMPROGRAMS\jvm"
  CreateShortcut "$SMPROGRAMS\jvm\Uninstall jvm.lnk" "$INSTDIR\uninstall.exe"
SectionEnd

; ---------- Section descriptions (hover tooltips) ----------
!insertmacro MUI_FUNCTION_DESCRIPTION_BEGIN
  !insertmacro MUI_DESCRIPTION_TEXT ${SecCore}     "jvm.exe and its uninstaller (required)"
  !insertmacro MUI_DESCRIPTION_TEXT ${SecConfig}   "Add jvm to PATH and enable instant `jvm use` in the current terminal (recommended)"
  !insertmacro MUI_DESCRIPTION_TEXT ${SecShortcut} "Create an uninstall shortcut under Start Menu > jvm"
!insertmacro MUI_FUNCTION_DESCRIPTION_END

; ---------- Uninstaller ----------
Section "Uninstall"
  ; Files
  Delete "$INSTDIR\jvm.exe"
  Delete "$INSTDIR\uninstall.exe"

  ; Start Menu
  Delete "$SMPROGRAMS\jvm\Uninstall jvm.lnk"
  RMDir "$SMPROGRAMS\jvm"

  ; Registry entries
  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\jvm"
  DeleteRegKey HKCU "Software\jvm"

  ; Ask whether to also remove user data (~/.jvm, holds installed JDKs).
  ; Default to No so we never wipe a working JDK collection without consent.
  MessageBox MB_YESNO|MB_DEFBUTTON2|MB_ICONQUESTION \
    "Also delete the user data folder ($PROFILE\.jvm)?$\r$\n$\r$\nThis removes ALL installed JDK versions.$\r$\nChoose No if you want to keep them." \
    /SD IDNO IDNO skipData
      RMDir /r "$PROFILE\.jvm"
      DetailPrint "Removed $PROFILE\.jvm"
  skipData:
    DetailPrint "Uninstall complete."

  ; Remove the (now empty) install directory
  RMDir "$INSTDIR"
SectionEnd
