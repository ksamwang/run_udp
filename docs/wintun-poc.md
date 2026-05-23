# Wintun PoC

## Dependency

The LAN product line uses the official Go module:

- `golang.zx2c4.com/wintun`
- pinned in `go.mod`

This avoids manually calling the Wintun DLL APIs in our code. The module still loads `wintun.dll` at runtime from the application directory or `System32`.

## Distribution

`build-all.bat` runs `scripts/fetch-wintun.ps1`, downloads the official Wintun package, verifies its SHA256, and extracts `bin/amd64/wintun.dll` into `dist`.

The LAN installer and release zip must include:

- `UDPTunnelLAN.exe`
- `wintun.dll`
- `lan.json.example`

Runtime source:

- Wintun package: `https://www.wintun.net/builds/wintun-0.14.1.zip`
- SHA256: `07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51`
- upstream license file
- pinned module version

## Privileges

Creating the adapter and changing IPv4 address, MTU and route require administrator privileges.

The production model remains:

- Windows Service performs adapter creation and network configuration.
- Tray process only displays status and sends control commands.

## PoC Command

Run from an elevated terminal:

```powershell
.\UDPTunnelLAN.exe -wintun-poc -wintun-ip 172.16.10.250 -wintun-cidr 172.16.10.0/24 -wintun-mtu 1280
```

The PoC creates or opens adapter `UDP Tunnel LAN`, configures IPv4, sets MTU and adds an active route.

## Current Boundary

This stage validates adapter open/create, IPv4 configuration and packet read/write API wrapping.

Route conflict detection, cleanup policy, service lifecycle and sleep/network-change recovery are handled in stage 4.5.
