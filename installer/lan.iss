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
Source: "{#MySourceDir}\wintun.dll"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#MySourceDir}\lan.json.example"; DestDir: "{app}"; DestName: "lan.json"; Flags: onlyifdoesntexist

[Icons]
Name: "{group}\UDP Tunnel LAN"; Filename: "{app}\{#MyAppExeName}"; Parameters: "-tray -config ""{app}\lan.json"""
Name: "{group}\UDP Tunnel LAN Logs"; Filename: "{app}"

[Run]
Filename: "{app}\{#MyAppExeName}"; Parameters: "-install-service -config ""{app}\lan.json"""; Flags: runhidden waituntilterminated
Filename: "{app}\{#MyAppExeName}"; Parameters: "-start-service"; Flags: runhidden waituntilterminated
Filename: "{app}\{#MyAppExeName}"; Parameters: "-tray -config ""{app}\lan.json"""; Flags: nowait postinstall skipifsilent

[UninstallRun]
Filename: "{app}\{#MyAppExeName}"; Parameters: "-stop-service"; Flags: runhidden waituntilterminated skipifdoesntexist; RunOnceId: "StopUDPTunnelLANService"
Filename: "{app}\{#MyAppExeName}"; Parameters: "-uninstall-service"; Flags: runhidden waituntilterminated skipifdoesntexist; RunOnceId: "UninstallUDPTunnelLANService"
Filename: "{cmd}"; Parameters: "/C taskkill /F /T /IM {#MyAppExeName}"; Flags: runhidden waituntilterminated; RunOnceId: "KillUDPTunnelLANProcesses"

[Code]
var
  LANConfigPage: TInputQueryWizardPage;

function DefaultLANServerHTTP(): String;
begin
  Result := ExpandConstant('{param:LANServerHTTP|http://api.tunnel.wanglv.top}');
end;

function LANConfigPath(): String;
begin
  Result := ExpandConstant('{app}\lan.json');
end;

function ShouldWriteLANConfig(): Boolean;
begin
  Result := not FileExists(LANConfigPath());
end;

procedure InitializeWizard();
begin
  LANConfigPage := CreateInputQueryPage(
    wpSelectDir,
    'LAN 控制面配置',
    '配置 UDP Tunnel LAN 服务端地址',
    '请输入 LAN 客户端连接的控制面 HTTP 地址。升级安装时，如果已存在 lan.json，安装器不会覆盖原配置。'
  );
  LANConfigPage.Add('控制面地址:', False);
  LANConfigPage.Values[0] := DefaultLANServerHTTP();
end;

function NextButtonClick(CurPageID: Integer): Boolean;
var
  Value: String;
begin
  Result := True;
  if CurPageID = LANConfigPage.ID then begin
    Value := Trim(LANConfigPage.Values[0]);
    if (Value <> '') and (Pos('http://', Lowercase(Value)) <> 1) and (Pos('https://', Lowercase(Value)) <> 1) then begin
      MsgBox('控制面地址必须以 http:// 或 https:// 开头。', mbError, MB_OK);
      Result := False;
    end;
  end;
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  Value: String;
  JSON: String;
begin
  if CurStep = ssPostInstall then begin
    if ShouldWriteLANConfig() then begin
      Value := Trim(LANConfigPage.Values[0]);
      JSON := '{'#13#10 +
        '  "server_http": "' + Value + '",'#13#10 +
        '  "log_level": "info"'#13#10 +
        '}'#13#10;
      SaveStringToFile(LANConfigPath(), JSON, False);
    end;
  end;
end;
