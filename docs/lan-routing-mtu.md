# LAN Routing and MTU Policy

## Route Conflict Detection

LAN clients inspect local IPv4 routes before configuring the virtual network.

If the requested virtual CIDR overlaps an existing local route, the client must not overwrite the route. It reports the conflict and uses the next available `/24` candidate.

Default candidates:

- preferred: `172.16.10.0/24`
- fallback range: `172.16.10.0/24` through `172.16.254.0/24`

The server-side model supports the selected CIDR through LAN status reporting. Admin UI can show `route_conflict` and `selected_cidr`.

## MTU and MSS

Default TUN MTU is `1280`.

TCP MSS policy:

- clamp target: `min(mtu - 40, 1200)`
- lower bound: `536`

The PoC records MSS in status. Actual TCP SYN MSS rewrite is implemented in the packet router stage, where IPv4/TCP parsing already exists.

## Cleanup

The Wintun PoC removes the active route it created before exit.

Production cleanup must be owned by the Windows Service:

- service stop: remove active LAN route
- uninstall: remove route and service/tray entries
- crash/reboot: reconcile desired route on next service start

## Recovery

Sleep/wake, adapter disable/enable and network changes should trigger:

- route table re-check
- selected CIDR recalculation
- Wintun adapter reconfiguration
- LAN status update

The event subscription and service lifecycle hooks are implemented in later runtime stages.
