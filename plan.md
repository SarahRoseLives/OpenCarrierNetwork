# OCN — OpenCarrier.Network

## Vision

A federated telephone network anyone can operate. Anyone can run an exchange,
own an area code, and join one shared numbering space. Numbers are bound to a
kSIM Ed25519 keypair; calls are peer-to-peer over WebRTC (DTLS-SRTP). Stored
voicemail and messages are encrypted at rest by the server today; recipient-only
E2E encryption for stored media is a stated later goal.

---

## Where we are (live today)

- **Registry** at `opencarrier.network` (HTTPS / Let's Encrypt website + `/admin`):
  area-code pool (200–999), routing table, embedded TURN relay, and delegated
  FCM push (call / voicemail / message wake-ups).
- **Exchange 440 · OpenCarrier** (`ocn-first`): registered users, WebRTC
  signaling, federation to the registry, hosted network services, per-user
  voicemail and messaging mailboxes.
- **Network services**: `800-776-6001` party-room conference (server-side
  libopus mixing), announcement lines, and the `*01` echo test.
- **Softphone** (Android + Linux): P2P calls, visual voicemail, 1:1 text + image
  messaging with offline delivery, contacts, call history, QR/deep-link
  provisioning, and notifications.

---

## Where we're going

**Next**
- SIP/ATA phone interop (server-level SIP registrar + WebRTC↔SIP media bridge).
- PIN/DTMF voicemail boxes so analog/ATA phones can check mailboxes.
- Federation hardening: inter-server TLS + auth (dev links are plaintext today).
- Public registry exchange-directory / live-status API surfaced on the website.

**Later / backlog**
- Group messaging (cut from messaging v1).
- True recipient-only E2E for stored voicemail/messages.
- kSIM backup/export; more desktop/mobile platforms.
- Anti-spam: per-exchange rate limiting, per-user block lists.

More exchanges joining the registry (440 is the first).

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     OCN Registry (opencarrier.network)        │
│  Area-code pool │ Routing │ ResolveService │ TURN │ FCM push  │
└───────┬───────────────────────────────┬─────────────────────┘
        │ gRPC (TLS in prod)            │ gRPC
 ┌──────▼────────┐               ┌──────▼────────┐
 │  Exchange 440 │◄─────────────►│  Your Exchange │
 │  ocnserver    │  BridgeCall    │  ocnserver    │
 │  Go + pion    │  (calls)       │  Go + pion    │
 └──┬───────┬────┘  DeliverDM     └──┬───────┬────┘
    │       │      (messages)        │       │
 WebRTC    │  services/voicemail  WebRTC    │
   P2P     │  (server media)        P2P     │
   calls   │  conference/echo               │
      Softphone (Android/Linux)       (planned) ATA/SIP phones
      + text/image messaging
```

Key points:
- Client↔client calls are **P2P WebRTC**; the server only relays signaling.
- The server terminates media **only** for hosted services (conference rooms,
  voicemail recording/playback, announcements).
- Messaging is relayed by the sender's exchange and queued on the recipient's
  home exchange when offline.
- SIP/ATA interop is planned, not yet present (dashed above).

---

## Numbering Plan

| Range | Purpose | Owner |
|-------|---------|-------|
| `800-XXX-XXXX` | Network services (conference, announcement) | Registry |
| `900-XXX-XXXX` | Network services (reserved) | Registry |
| `200-999-XXX-XXXX` | Exchange area codes | Assigned on join |

- **Local dialing**: `XXX-XXXX` (7 digits, same exchange).
- **Cross-exchange**: `XXX-XXX-XXXX` (full 10-digit number).
- **No portability**: numbers belong to the issuing exchange.

---

## Components

### Component 1: kSIM Identity
- ✅ Keypair generation (Ed25519), signing, challenge-response auth.
- ✅ Number is bound to the kSIM public key at the issuing exchange.
- ✅ Provisioning via admin-issued QR / `ocnksim://` deep links.
- ✅ Per-connection identity: once registered, the WebSocket is trusted for that
  user (no per-message signatures today).
- ⏳ Backup/export of kSIM identity.

### Component 2: OCN Registry
- ✅ Area-code assignment (200–999), routing table, service-number resolution.
- ✅ Embedded TURN relay + ICE server advertisement.
- ✅ Delegated FCM push (call / voicemail / message) using a shared Firebase
  service account.
- ✅ HTTPS website (Let's Encrypt) + admin panel.
- ✅ gRPC (TLS): `RegisterOCNServer`, `DeregisterOCNServer`, `ListOCNServers`,
  `GetRoute`, `ResolveService`, `GetICECandidates`, `PushDevice`.
- ⏳ Public exchange-directory / live-status API for the website.

### Component 3: OCNServer (Go exchange)
- ✅ User registration/provisioning, number pool allocation, SQLite store.
- ✅ WebSocket JSON signaling (calls, voicemail, messaging, ICE).
- ✅ Caller ID (display name) on outbound/inbound signaling.
- ✅ Local + federated calls over `BridgeCall` (gRPC streaming CallEvents).
- ✅ Hosted 800/900 network services: conference, announcement, echo (`*01`).
- ✅ Voicemail: record on offline / no-answer / declined, notify, visual playback
  in the app; audio encrypted at rest (AES-256-GCM, server master secret).
- ✅ Messaging: 1:1 text + image, online relay + encrypted offline outbox +
  register-time flush; federated via `DeliverDM`.
- ✅ Admin panel (lines, provisioning tokens, federation, hosted services).
- ⏳ SIP registrar + WebRTC↔SIP media bridge (planned for ATA/analog phones).
- ⏳ DTMF/PIN voicemail boxes.

### Component 4: Softphone (Flutter)
- Platforms: Android ✅, Linux ✅; Windows/macOS/iOS ⏳.
- ✅ Dialer (7/10-digit smart formatting), WebRTC calls, ringback, reconnect.
- ✅ Caller ID resolution (contacts → server name → formatted number).
- ✅ Contacts + call history (local JSON stores).
- ✅ Visual voicemail (list, playback, unread badge, notifications).
- ✅ 1:1 text + image messaging (threads, delivered state, offline delivery,
  unread badges, notifications).
- ✅ Incoming-call + new-message/voicemail notifications (FCM/local).
- ⏳ In-call DTMF sending (keypad dials digits) — planned with SIP/ATA work.
- ⏳ kSIM backup/export.

### Component 5: Protocol inventory
- `proto/registry.proto` — registry ↔ exchange (above RPC list).
- `proto/ocnserver.proto` — exchange ↔ exchange: `BridgeCall` (calls),
  `DeliverDM` (messages).
- `proto/common.proto` — shared messages.
- Client ↔ exchange is **JSON over WebSocket** (`signaling/messages.go`); the
  aspirational `signaling.proto` / `voicemail.proto` are not used.

---

## Tech Stack

| Component | Stack |
|-----------|-------|
| Registry | Go, gRPC, SQLite, embedded TURN (pion), Firebase FCM |
| Exchange | Go, pion/webrtc, libopus (cgo mixing), SQLite |
| Softphone | Flutter, flutter_webrtc, audioplayers, image_picker |
| Identity | Ed25519 keypairs (kSIM) |
| Signaling | WebSocket JSON (client↔exchange) |
| Media | WebRTC DTLS-SRTP (P2P calls; server media for services) |
| At-rest storage | AES-256-GCM, keys derived from a per-server master secret |
| Transport | gRPC + TLS (registry↔exchange, prod), wss planned (client↔exchange) |

---

## Roadmap

**Done**
- Registry + federation (routing, TURN, FCM relay, website, admin).
- Softphone core (provisioning, dialer, calls, caller ID, contacts, history).
- Voicemail (record on offline/no-answer/decline, notifications, visual
  playback, encrypted at rest).
- Messaging (1:1 text + images, offline queue, delivered state, federated).
- Network services (conference, announcement, echo) on 800/900.
- Homepage redesign + GitHub repo hygiene.

**Next**
- [ ] SIP/ATA interop (SIP registrar + media bridge).
- [ ] PIN/DTMF voicemail boxes.
- [ ] Inter-server TLS/auth hardening.
- [ ] Public exchange directory / live-status API + website surfacing.
- [ ] Onboard additional exchanges.

**Later**
- [ ] Group messaging.
- [ ] Recipient-only E2E for stored voicemail/messages.
- [ ] kSIM backup/export; more platforms.
- [ ] Anti-spam (rate limiting, block lists).

---

## Notes

- Inter-server links are plaintext in development; production is to be hardened
  to TLS + per-server auth.
- The per-client WS send buffer drops messages when full; inbound messages are
  re-delivered on next register until acked.
- Inline image messages are capped at 4 MB; undelivered outbox entries expire
  after 7 days.
