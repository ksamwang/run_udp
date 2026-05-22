# Wintun PoC

## Dependency

The LAN product line uses the official Go module:

- `golang.zx2c4.com/wintun`
- pinned in `go.mod`

This avoids manually loading `wintun.dll` in our code. The module is responsible for interacting with the Wintun runtime.

## Distribution

For the PoC, `UDPTunnelLAN.exe` links the Wintun Go module and does not bundle a separate installer yet.

Before production packaging, the LAN installer must verify:

- Wintun runtime files required by `golang.zx2c4.com/wintun`
- upstream license file
- pinned module version
- SHA256 of any bundled binary asset

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
