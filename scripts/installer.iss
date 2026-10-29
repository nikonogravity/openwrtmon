; Inno Setup Script for OpenWRT Monitor
; To compile: Download Inno Setup from https://jrsoftware.org/isdl.php, open this file and press F9

#define MyAppName "OpenWRT Monitor"
#define MyAppVersion "2.0.0"
#define MyAppPublisher "OpenWRT Monitor Team"
#define MyAppExeName "openwrtmon.exe"

[Setup]
AppId={{9F82A4C1-2854-4B2E-893C-E7E1D8D20341}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
DefaultDirName={localappdata}\Programs\OpenWRTMonitor
DisableProgramGroupPage=yes
OutputBaseFilename=OpenWRTMonitor-Setup
Compression=lzma
SolidCompression=yes
WizardStyle=modern
PrivilegesRequired=lowest
OutputDir=..\dist

[Languages]
Name: "russian"; MessagesFile: "compiler:Languages\Russian.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked
Name: "startup"; Description: "Запускать OpenWRT Monitor при входе в Windows"; GroupDescription: "Автозагрузка:"

[Files]
Source: "..\dist\openwrtmon.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\config.ini"; DestDir: "{app}"; Flags: onlyifdoesntexist uninsneveruninstall

[Icons]
Name: "{autoprograms}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon
Name: "{userstartup}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: startup

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "{cm:LaunchProgram,{#StringChange(MyAppName, '&', '&&')}}"; Flags: nowait postinstall skipifsilent

