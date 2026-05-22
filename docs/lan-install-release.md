# UDP Tunnel LAN Install and Release

## Build Artifacts

`build-all.bat` builds these LAN artifacts:

- `dist\UDPTunnelLAN.exe`
- `dist\lan.json.example`
- `dist\UDPTunnelLAN-windows-amd64-<tag>.zip` during release packaging
- `dist\udp-tunnel-lan-<version>-setup.exe` when Inno Setup is installed

`release.bat <version>` uploads the LAN zip and LAN installer when they exist.

## Windows Install Layout

The LAN product line is installed separately from the port-forwarding client.

- Install directory: `%ProgramFiles%\UDP Tunnel LAN`
- Config file: `%ProgramFiles%\UDP Tunnel LAN\lan.json`
- Log file: `%ProgramFiles%\UDP Tunnel LAN\UDPTunnelLAN.log`
- Previous log snapshot: `%ProgramFiles%\UDP Tunnel LAN\UDPTunnelLAN.log.1`
- Windows service name: `UDPTunnelLAN`
- Tray Run key: `HKLM\Software\Microsoft\Windows\CurrentVersion\Run\UDPTunnelLANTray`

The existing port-forwarding client keeps using `%ProgramFiles%\UDP Tunnel`, service `UDPTunnelAgent`, and its own tray Run key.

## Service Lifecycle

The LAN installer runs:

```powershell
UDPTunnelLAN.exe -install-service -config "%ProgramFiles%\UDP Tunnel LAN\lan.json"
UDPTunnelLAN.exe -start-service
```

Uninstall runs:

```powershell
UDPTunnelLAN.exe -stop-service
UDPTunnelLAN.exe -uninstall-service
```

The service is configured as automatic start and has Windows service recovery configured to restart after failures.

## Version Compatibility

The LAN bootstrap response includes a numeric `version` and a `capabilities` list. First-version compatibility policy:

- A LAN client must reject bootstrap responses with a future unsupported major protocol version.
- A LAN client must check required capabilities before enabling packet routing.
- Server-side LAN API changes should remain additive while `version == 1`.
- Breaking LAN API or packet protocol changes require incrementing the bootstrap protocol version.

## Reboot Acceptance

Before publishing a production LAN release:

- Install `udp-tunnel-lan-<version>-setup.exe` as administrator.
- Confirm Windows service `UDPTunnelLAN` is running.
- Reboot Windows.
- Confirm `UDPTunnelLAN` starts automatically.
- Confirm `UDPTunnelLAN.log` contains startup logs after reboot.
- Confirm uninstall removes the service and tray Run key.
