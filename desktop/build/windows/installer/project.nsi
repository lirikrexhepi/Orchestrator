Unicode true

####
## Orchestrator Windows Installer (NSIS)
##
## Per-user installation into %LOCALAPPDATA%\Programs\Orchestrator:
## - No UAC elevation required for initial install or silent in-app updates.
## - Preserves custom install directory across upgrades via InstallDirRegKey.
## - Gracefully waits for a running Orchestrator.exe to release its file lock
##   before overwriting (essential for zero-glitch silent /S auto-updates).
## - Supports /S /RELAUNCH so the in-app updater exits cleanly, swaps the
##   binary, updates metadata/shortcuts, and relaunches the new version.
## - Preserves %APPDATA%\composer (chats, history, accounts, settings) on both
##   upgrades and uninstall.
####

!ifdef REQUEST_EXECUTION_LEVEL
    !undef REQUEST_EXECUTION_LEVEL
!endif
!define REQUEST_EXECUTION_LEVEL "user"

!ifdef WAILS_INSTALL_SCOPE
    !undef WAILS_INSTALL_SCOPE
!endif
!define WAILS_INSTALL_SCOPE "user"

!include "wails_tools.nsh"

# Version info requires 4 numeric components (major.minor.patch.build)
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

ManifestDPIAware true

!include "MUI2.nsh"
!include "LogicLib.nsh"

; Inject FileFunc helpers used for silent /RELAUNCH handling (FileFunc.nsh is included by wails_tools.nsh).
!insertmacro GetParameters
!insertmacro GetOptions

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN "$INSTDIR\${PRODUCT_EXECUTABLE}"
!define MUI_FINISHPAGE_RUN_TEXT "Launch ${INFO_PRODUCTNAME}"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

Name "${INFO_PRODUCTNAME} ${INFO_PRODUCTVERSION}"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe"
InstallDir "$LOCALAPPDATA\Programs\${INFO_PRODUCTNAME}"
InstallDirRegKey HKCU "${UNINST_KEY}" "InstallLocation"
ShowInstDetails nevershow
ShowUninstDetails nevershow

; Wait until a running Orchestrator.exe finishes exiting so we never hit a
; locked-file error when overwriting during an in-place upgrade or uninstall.
!macro orchestrator.waitForUnlock TARGET_EXE
    ${If} ${FileExists} "${TARGET_EXE}"
        StrCpy $R0 0
        ${Do}
            ClearErrors
            Delete "${TARGET_EXE}"
            ${IfNot} ${Errors}
                ${Break}
            ${EndIf}
            ; After 2s (8 * 250ms), ask any lingering instance to close gracefully.
            ${If} $R0 == 8
                nsExec::ExecToLog '"$SYSDIR\taskkill.exe" /IM "${PRODUCT_EXECUTABLE}"'
                Pop $R1
            ${EndIf}
            ; After 6s (24 * 250ms), force-terminate so the binary swap cannot stall.
            ${If} $R0 == 24
                nsExec::ExecToLog '"$SYSDIR\taskkill.exe" /IM "${PRODUCT_EXECUTABLE}" /T /F'
                Pop $R1
            ${EndIf}
            IntOp $R0 $R0 + 1
            ${If} $R0 >= 60
                ${Break}
            ${EndIf}
            Sleep 250
        ${Loop}
    ${EndIf}
!macroend

Function .onInit
    !insertmacro wails.checkArchitecture
FunctionEnd

Section "Install"
    !insertmacro wails.setShellContext

    !insertmacro wails.webview2runtime

    SetOutPath $INSTDIR

    !insertmacro orchestrator.waitForUnlock "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.files

    ; Always keep Start Menu shortcut up to date. On silent auto-updates, only
    ; refresh the Desktop shortcut if the user hasn't deleted it.
    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    IfSilent 0 createDesktopShortcut
        ${If} ${FileExists} "$DESKTOP\${INFO_PRODUCTNAME}.lnk"
            CreateShortcut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
        ${EndIf}
        Goto afterDesktopShortcut
    createDesktopShortcut:
        CreateShortcut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    afterDesktopShortcut:

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    !insertmacro wails.writeUninstaller

    ; Additional Windows Apps & Features metadata
    SetRegView 64
    WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
    WriteRegStr HKCU "${UNINST_KEY}" "URLInfoAbout" "https://github.com/lirikrexhepi/Orchestrator"
    WriteRegDWORD HKCU "${UNINST_KEY}" "NoModify" 1
    WriteRegDWORD HKCU "${UNINST_KEY}" "NoRepair" 1

    ; Refresh shell icon cache so updated icons/versions appear immediately
    System::Call 'shell32::SHChangeNotify(i 0x08000000, i 0, i 0, i 0)'

    ; In silent auto-update mode (/S /RELAUNCH), relaunch the freshly installed app
    IfSilent 0 skipSilentRelaunch
        ${GetParameters} $R2
        ClearErrors
        ${GetOptions} $R2 "/RELAUNCH" $R3
        ${IfNot} ${Errors}
            Exec '"$INSTDIR\${PRODUCT_EXECUTABLE}"'
        ${EndIf}
    skipSilentRelaunch:
SectionEnd

Section "uninstall"
    !insertmacro wails.setShellContext

    !insertmacro orchestrator.waitForUnlock "$INSTDIR\${PRODUCT_EXECUTABLE}"

    ; Remove WebView2 runtime cache for this binary, but keep %APPDATA%\composer
    ; (chats, history, accounts, settings) intact.
    RMDir /r "$APPDATA\${PRODUCT_EXECUTABLE}"
    RMDir /r "$LOCALAPPDATA\${PRODUCT_EXECUTABLE}"

    Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"
    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    !insertmacro wails.deleteUninstaller

    ; Only remove $INSTDIR if empty so a custom folder with user files is never wiped.
    RMDir "$INSTDIR"

    System::Call 'shell32::SHChangeNotify(i 0x08000000, i 0, i 0, i 0)'
SectionEnd
