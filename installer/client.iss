#define MyAppName "UDP Tunnel"
#define MyAppExeName "client.exe"
#define MyServiceName "UDPTunnelAgent"

#ifndef MyAppVersion
  #define MyAppVersion "dev"
#endif
#ifndef MySourceDir
  #define MySourceDir "..\\dist"
#endif

[Setup]
AppId={{9D74F0B7-4D67-4B40-8AC7-89A4D73F2960}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
DefaultDirName={autopf}\UDP Tunnel
DefaultGroupName=UDP Tunnel
OutputBaseFilename=udp-tunnel-client-{#MyAppVersion}-setup
Compression=lzma
SolidCompression=yes
PrivilegesRequired=admin
ArchitecturesInstallIn64BitMode=x64
UninstallDisplayIcon={app}\{#MyAppExeName}

[Files]
Source: "{#MySourceDir}\client.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#MySourceDir}\client.json.example"; DestDir: "{app}"; DestName: "client.json"; Flags: onlyifdoesntexist

[Icons]
Name: "{group}\UDP Tunnel"; Filename: "{app}\{#MyAppExeName}"; Parameters: "-tray -config ""{app}\client.json"""
Name: "{group}\UDP Tunnel Settings"; Filename: "{app}\{#MyAppExeName}"; Parameters: "-tray -config ""{app}\client.json"""

[Run]
Filename: "{app}\{#MyAppExeName}"; Parameters: "-install-service -config ""{app}\client.json"""; Flags: runhidden waituntilterminated
Filename: "{app}\{#MyAppExeName}"; Parameters: "-start-service"; Flags: runhidden waituntilterminated
Filename: "{app}\{#MyAppExeName}"; Parameters: "-tray -config ""{app}\client.json"""; Flags: postinstall nowait skipifsilent

[UninstallRun]
Filename: "{app}\{#MyAppExeName}"; Parameters: "-stop-service"; Flags: runhidden waituntilterminated skipifdoesntexist
Filename: "{app}\{#MyAppExeName}"; Parameters: "-uninstall-service"; Flags: runhidden waituntilterminated skipifdoesntexist
