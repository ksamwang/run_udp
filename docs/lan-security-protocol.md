# UDP Tunnel LAN Security Protocol

## Scope

This document defines the first LAN data-plane security boundary. It is separate from the existing deployment PSK based secure frame used by the port-forwarding product line.

The LAN data plane uses device identity keys for authentication and short-lived packet session keys for encryption.

## Identity Keys

- Algorithm: Ed25519.
- Local private key file: `lan-identity.json`.
- Public key storage: server table `virtual_device_keys`.
- Public key format: base64-encoded Ed25519 public key bytes.
- Private key format: base64-encoded Ed25519 private key bytes.

The server stores the latest public key reported by LAN bootstrap. First version key rotation is implemented as re-bootstrap with a new public key. Later versions can add challenge signatures and admin approval.

## Peer Handshake

Each peer session uses a fresh X25519 ephemeral keypair.

Planned handshake messages:

- `lan_handshake_init`
  - `version`
  - `network_id`
  - `src_device`
  - `dst_device`
  - `session_id`
  - `x25519_ephemeral_public`
  - `timestamp`
  - `signature`
- `lan_handshake_accept`
  - `version`
  - `network_id`
  - `src_device`
  - `dst_device`
  - `session_id`
  - `x25519_ephemeral_public`
  - `timestamp`
  - `signature`

The signature is Ed25519 over the canonical handshake transcript. The transcript includes both device IDs, network ID, session ID, ephemeral public keys and timestamp. This binds the X25519 exchange to the device identity registered through bootstrap.

## Session Key Derivation

Both peers compute X25519 shared secret and derive directional packet keys using HKDF-SHA256.

Inputs:

- shared secret: X25519 result
- salt: `network_id || session_id || ordered(src_device, dst_device)`
- info:
  - `udp-tunnel-lan/session/v1/client-to-server`
  - `udp-tunnel-lan/session/v1/server-to-client`

Each direction gets an independent 32-byte key for ChaCha20-Poly1305.

## Packet Frame

Packet payloads are encrypted using independent LAN packet session crypto.

Frame fields:

- magic: `UDPL`
- version: `1`
- packet type
- flags
- network ID
- sequence number
- ciphertext
- auth tag

Additional authenticated data includes every cleartext header field. Payload is the raw IPv4 packet from the virtual adapter.

## Replay Protection

Each receiver tracks the highest accepted sequence number and a sliding replay window.

Rules:

- duplicate sequence is rejected
- sequence older than the replay window is rejected
- out-of-order packets inside the window are accepted once

## ACL Enforcement

ACL is enforced in three places:

- sender: avoids sending packets that local policy already denies
- receiver: mandatory final enforcement before writing to TUN
- server control plane: only returns peers and policy that the device is allowed to know

Default policy for the first version is same-group allow. Explicit deny rules take precedence.

## Current Implementation Boundary

This stage implements:

- packet session frame format
- ChaCha20-Poly1305 encryption/decryption
- directional key derivation primitive
- sequence based replay protection

The actual peer handshake transport and TUN integration are implemented in later stages.
