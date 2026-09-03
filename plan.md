# OCN — OpenCarrier.Network Game Plan

## Vision

A federated, encrypted phone network. Anyone can run an exchange, users get phone numbers via keypair identity (kSIM), voicemail is end-to-end encrypted, and calls route peer-to-peer when possible.

---

## Architecture

```
┌──────────────────────────────────────────────────────┐
│                   OCN Registry                        │
│  Area code pool │ Routing table │ STUN │ OCNServer dir │
└────────┬──────────────────────────┬──────────────────┘
         │ gRPC + TLS               │ gRPC + TLS
  ┌──────▼───────┐          ┌───────▼──────┐
  │  OCNServer A   │◄────────►│  OCNServer B   │
  │  Go server    │ WebRTC   │  Go server    │
  │  Area: 212    │  P2P     │  Area: 310    │
  └──┬──┬──┬──┬──┘          └──┬──┬──┬──┬──┘
     │  │  │  │                │  │  │  │
   SIP  │  │  SIP            SIP  │  │  SIP
     │  │  │  │                │  │  │  │
   ATA  │  │  ATA            ATA  │  │  ATA
      WebRTC │                   WebRTC │
         │   │                      │   │
     Softphone Voicemail        Softphone Voicemail
     (Flutter) (E2E enc)       (Flutter) (E2E enc)
```

---

## Numbering Plan

| Range | Purpose | Owner |
|-------|---------|-------|
| `800-XXX-XXXX` | OCN services (directory, echo test) | OCN Registry |
| `900-XXX-XXXX` | OCN premium/info services | OCN Registry |
| `200-999-XXX-XXXX` | Federated exchange area codes | Assigned on join |

- **Local dialing**: `XXX-XXXX` (7 digits, same exchange)
- **Cross-exchange**: `XXX-XXX-XXXX` (full number)
- **No portability**: Numbers belong to the issuing exchange

---

## Component 1: kSIM Identity

Keypair-based identity. No SIM card, no eSIM — just a cryptographic keypair that proves you are you.

### Lifecycle
1. User installs softphone → generates Ed25519 keypair
2. Private key encrypted locally with user passphrase
3. User picks an exchange, registers with public key
4. OCNServer verifies signature, issues phone number, stores binding
5. All future auth: exchange sends challenge, client signs with private key

### kSIM File (stored on device)
```json
{
  "version": 1,
  "keypair": {
    "public": "base64-ed25519-pubkey",
    "private": "base64-ed25519-privkey-encrypted"
  },
  "display_name": "Alice",
  "exchange": "exchange-a.ocn.network",
  "number": "212-555-1234",
  "registered_at": "2026-09-02T00:00:00Z"
}
```

### Caller ID
- Display name stored in kSIM and registered with exchange
- OCNServer includes caller display name in signaling metadata on outbound calls
- Recipient sees: `212-555-1234 (Alice)`

---

## Component 2: OCN Registry (Central)

**Stack**: Go, gRPC + REST gateway, SQLite/BoltDB

### Responsibilities
- **Area code assignment**: Pool of available 3-digit codes, assign to joining exchanges
- **Routing table**: `area_code → exchange gRPC endpoint + public key`
- **OCNServer directory**: Public list of all exchanges (name, area code, description)
- **STUN server**: Help WebRTC clients discover public endpoints
- **800/900 services**: Directory lookup, echo test

### gRPC API
```protobuf
service OCNRegistry {
  // OCNServer management
  rpc RegisterOCNServer(RegisterOCNServerRequest) returns (RegisterOCNServerResponse);
  rpc DeregisterOCNServer(DeregisterOCNServerRequest) returns (google.protobuf.Empty);
  rpc ListOCNServers(ListOCNServersRequest) returns (ListOCNServersResponse);

  // Routing
  rpc GetRoute(GetRouteRequest) returns (GetRouteResponse);

  // STUN
  rpc GetICECandidates(ICECandidateRequest) returns (ICECandidateResponse);

  // 800/900 services
  rpc HandleServiceCall(ServiceCallRequest) returns (ServiceCallResponse);
}
```

### Data Model
```
OCNServer {
  area_code:      string (3 digits, unique)
  name:           string
  description:    string
  server_address: string (gRPC endpoint)
  public_key:     bytes (exchange identity key)
  registered_at:  timestamp
  status:         ACTIVE | SUSPENDED
}
```

---

## Component 3: OCNServer (Go)

**Stack**: Go, pion/webrtc, embedded SIP, gRPC client, SQLite/BoltDB

### Responsibilities
- **User registration**: Verify kSIM signatures, issue phone numbers from local pool
- **Local routing**: Connect two local users via WebRTC signaling relay
- **Cross-exchange routing**: Query registry for route, bridge to remote exchange
- **SIP/ATA**: Embedded SIP registrar + proxy for analog adapters
- **Voicemail**: Store E2E-encrypted voicemail (can't be read by server)
- **Caller ID**: Attach display name to all outbound call signaling

### Directory Structure
```
ocnserver/
├── cmd/exchange/main.go
├── internal/
│   ├── auth/          # kSIM challenge-response verification
│   ├── callerid/      # Display name resolution
│   ├── registry/      # gRPC client to OCN registry
│   ├── routing/       # Local vs cross-exchange decision
│   ├── signaling/     # WebSocket signaling server
│   ├── sip/           # SIP registrar + proxy
│   ├── numbers/       # 7-digit number allocation
│   ├── voicemail/     # Encrypted voicemail storage + retrieval
│   ├── stun/          # STUN relay
│   └── store/         # SQLite/BoltDB (users, numbers, voicemail metadata)
├── proto/
└── config/
```

### Call Flow — Local
```
1. Caller → WebSocket → OCNServer: "call 555-1234" (signed with kSIM)
2. OCNServer verifies signature, looks up callee
3. OCNServer relays SDP offer/answer between caller and callee
4. WebRTC P2P established directly (DTLS-SRTP)
5. Caller ID: OCNServer injects caller display_name into signaling
```

### Call Flow — Cross-OCNServer
```
1. Caller dials 310-555-6789
2. OCNServer A extracts area code "310", queries registry for route
3. Registry returns OCNServer B's gRPC endpoint
4. OCNServer A → gRPC → OCNServer B: "incoming call for 555-6789"
5. OCNServer B locates callee, relays SDP back through chain
6. WebRTC P2P established between phones (or TURN relay if NAT blocks)
7. All media encrypted (DTLS-SRTP)
```

### Voicemail — E2E Encrypted
```
1. Callee doesn't answer → OCNServer prompts caller to leave message
2. Audio recorded by exchange
3. OCNServer encrypts audio with callee's public key (from kSIM)
4. Encrypted blob stored on exchange disk
5. When callee's phone comes online:
   a. OCNServer notifies phone of pending voicemail
   b. Phone requests encrypted blob
   c. Phone decrypts locally with private key
   d. OCNServer never has plaintext — zero-knowledge storage
```

### SIP/ATA Integration
- ATA registers with exchange using SIP credentials (derived from kSIM registration)
- Inbound: WebRTC call → SIP INVITE to ATA
- Outbound: ATA SIP INVITE → WebRTC signaling to exchange
- Caller ID passed through SIP headers

---

## Component 4: Softphone (Flutter)

**Stack**: Flutter, Dart, flutter_webrtc, pointycastle (crypto)

### Platforms
| Platform | v1 | Future |
|----------|-----|--------|
| Windows  | ✅ |  |
| Linux    | ✅ |  |
| Android  | ✅ |  |
| macOS    |  | ✅ |
| iOS      |  | ✅ |

### Features
- **kSIM management**: Generate, backup, import/export keypair
- **OCNServer registration**: Browse exchange directory, register with one
- **Dialer**: 7-digit local, 10-digit cross-exchange, smart formatting
- **Caller ID**: Set display name, see caller info on incoming calls
- **In-call**: Mute, speaker, hold, DTMF
- **Voicemail**: Retrieve + decrypt E2E voicemail locally
- **Contacts**: Local contact book
- **Call history**: Incoming/outgoing/missed with caller ID

### Directory Structure
```
softphone/
├── lib/
│   ├── main.dart
│   ├── core/
│   │   ├── ksim/          # Keypair gen, signing, encrypted storage
│   │   ├── signaling/     # WebSocket client
│   │   ├── webrtc/        # Call management
│   │   ├── crypto/        # Voicemail decryption, message signing
│   │   └── config/        # OCNServer URL, kSIM path
│   ├── features/
│   │   ├── dialer/        # Dial pad + number input
│   │   ├── call/          # Active call screen + caller ID display
│   │   ├── voicemail/     # Voicemail list + playback
│   │   ├── contacts/      # Contact management
│   │   ├── history/       # Call log
│   │   ├── registration/  # OCNServer browser + kSIM registration
│   │   └── settings/      # kSIM info, display name, exchange
│   └── ui/                # Shared widgets, theme
├── android/
├── linux/
└── windows/
```

---

## Component 5: Proto Definitions

```
proto/
├── registry.proto       # Registry ↔ OCNServer (area codes, routing, STUN)
├── exchange.proto       # OCNServer ↔ OCNServer (cross-exchange calls)
├── signaling.proto      # Client ↔ OCNServer (WebSocket, JSON or protobuf)
├── voicemail.proto      # Voicemail upload/download/notification
└── common.proto         # PhoneNumber, kSIMId, DisplayName, etc.
```

---

## v1 Milestone — Single OCNServer + Softphone

### Phase 1: Foundation
- [ ] Define all proto files
- [ ] kSIM library (Go) — keypair gen, signing, verification
- [ ] kSIM library (Dart) — keypair gen, signing, encrypted storage
- [ ] OCNServer server skeleton (Go, config, gRPC server)

### Phase 2: OCNServer Core
- [ ] User registration (kSIM verify → issue number)
- [ ] Local number pool allocation (7-digit)
- [ ] SQLite store (users, numbers, display names)
- [ ] WebSocket signaling server
- [ ] Caller ID: store + attach display names to signaling

### Phase 3: Softphone
- [ ] Flutter project (Windows + Linux + Android)
- [ ] kSIM generation + passphrase-encrypted storage
- [ ] OCNServer browser + registration flow
- [ ] Dialer UI (smart 7/10 digit input)
- [ ] WebRTC call initiation + reception
- [ ] Caller ID display on incoming calls
- [ ] In-call UI (mute, hangup, speaker)
- [ ] Call history with caller ID

### Phase 4: Voicemail
- [ ] OCNServer: voicemail prompt on unanswered calls
- [ ] OCNServer: encrypt audio with callee's public key, store blob
- [ ] OCNServer: notification to callee when voicemail pending
- [ ] Softphone: voicemail list, fetch encrypted blob, decrypt + play locally

### Phase 5: SIP/ATA
- [ ] Embedded SIP registrar in exchange
- [ ] SIP ↔ WebRTC media bridge
- [ ] ATA registration with kSIM-derived credentials
- [ ] Caller ID passthrough via SIP headers

### Phase 6: Registry + Federation
- [ ] OCN Registry server (area code assignment, routing, STUN)
- [ ] OCNServer directory (public list endpoint)
- [ ] OCNServer registration with registry
- [ ] Cross-exchange call routing
- [ ] 800 number: Directory service (browse exchanges)
- [ ] 800 number: Echo test

---

## Tech Stack Summary

| Component | Stack |
|-----------|-------|
| Registry | Go, gRPC, SQLite/BoltDB, STUN |
| OCNServer | Go, gRPC, pion/webrtc, embedded SIP, SQLite/BoltDB |
| Softphone | Flutter, Dart, flutter_webrtc, pointycastle |
| Identity | Ed25519 keypairs (kSIM) |
| Signaling | WebSocket (JSON or protobuf) |
| Media | WebRTC (DTLS-SRTP), SRTP for SIP |
| Voicemail | AES-256 encrypted with recipient's public key |
| Transport | gRPC + TLS (registry↔exchange), WSS (client↔exchange) |

---

## Spam Prevention (Deferred)

Not in v1. Future options:
- OCNServer-level rate limiting per caller
- Block lists per user
- Reputation scoring across exchanges
- Challenge-response for unknown callers
