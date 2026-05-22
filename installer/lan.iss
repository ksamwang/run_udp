#define MyAppName "UDP Tunnel LAN"
#define MyAppExeName "UDPTunnelLAN.exe"
#define MyServiceName "UDPTunnelLAN"

#ifndef MyAppVersion
  #define MyAppVersion "dev"
#endif
#ifndef MySourceDir
  #define MySourceDir "..\\dist"
#endif

[Setup]
AppId={{F74F228A-072D-4CE0-95BA-35B566D7B019}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
DefaultDirName={autopf}\UDP Tunnel LAN
DefaultGroupName=UDP Tunnel LAN
OutputBaseFilename=udp-tunnel-lan-{#MyAppVersion}-setup
Compression=lzma
SolidCompression=yes
PrivilegesRequired=admin
ArchitecturesInstallIn64BitMode=x64
UninstallDisplayIcon={app}\{#MyAppExeName}

[Files]
Source: "{#MySourceDir}\{#MyAppExeName}"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#MySourceDir}\lan.json.example"; DestDir: "{app}"; DestName: "lan.json"; Flags: onlyifdoesntexist

[Icons]
Name: "{group}\UDP Tunnel LAN"; Filename: "{app}\{#MyAppExeName}"; Parameters: "-tray -config ""{app}\lan.json"""
Name: "{group}\UDP Tunnel LAN Logs"; Filename: "{app}"

[Run]
Filename: "{app}\{#MyAppExeName}"; Parameters: "-install-service -config ""{app}\lan.json"""; Flags: runhidden waituntilterminated
Filename: "{app}\{#MyAppExeName}"; Parameters: "-start-service"; Flags: runhidden waituntilterminated
Filename: "{app}\{#MyAppExeName}"; Parameters: "-tray -config ""{app}\lan.json"""; Flags: nowait postinstall skipifsilent

[UninstallRun]
Filename: "{app}\{#MyAppExeName}"; Parameters: "-stop-service"; Flags: runhidden waituntilterminated skipifdoesntexist
Filename: "{app}\{#MyAppExeName}"; Parameters: "-uninstall-service"; Flags: runhidden waituntilterminated skipifdoesntexist
