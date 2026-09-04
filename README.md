# OpenCarrier.Network (OCN)

> A telephone network anyone can operate.

OCN is an open, federated telephone network. Anyone can run an **exchange**
(server), own an area code, and join one shared numbering space. Numbers stay
familiar (`NPA-NXX-XXXX`); the transport underneath is modern Internet
technology — WebRTC calls, JSON-over-WebSocket signaling, and federated
messaging.

Live at **[opencarrier.network](https://opencarrier.network)** — get a number at
**[/join](https://opencarrier.network/join)**.

## What it is

- **Federated**: exchanges register with a central *registry* that assigns area
  codes and routes calls/messages between them. No single company owns the
  network.
- **Phone-like**: 7-digit local dialing within an exchange, full 10-digit
  `NPA-NXX-XXXX` across exchanges.
- **Identity**: each device holds an Ed25519 **kSIM** keypair bound to its
  number; provisioning is via QR / `ocnksim://` links minted by an exchange.
- **Private by default**: calls are P2P over WebRTC (DTLS-SRTP); stored
  voicemail and messages are encrypted at rest.

## What works today

- Federated voice calls between exchanges (over the wire: 440 ↔ 216).
- WebRTC softphone for **Android** and **Linux**.
- 1:1 **text + image messaging** with offline delivery, across exchanges.
- **Visual voicemail** with message notifications.
- Hosted services on 800/900 numbers: the party-room conference
  (`800-776-6001`), announcement lines, and the `*01` echo test.

## Repo layout

```
proto/           Shared protobuf definitions (registry, ocnserver)
registry_server/ The registry: area-code pool, routing, TURN, FCM, website
ocnserver/       An exchange (Go): signaling, calls, voicemail, messaging
softphone/       The Flutter softphone (Android + Linux)
scripts/         Proto generation etc.
plan.md          Living roadmap
```

## Run your own exchange

1. Build/run an `ocnserver` pointed at the registry
   (`registry_address: opencarrier.network:7443`).
2. Claim an area code from the **200–999** pool (800/900 are reserved for
   network services).
3. Provision softphones from the admin panel or the self-service `GET /join`
   page.

See [plan.md](plan.md) for the architecture and roadmap.

## Status

Early alpha, live with two exchanges. Next up: SIP/ATA interop, DTMF voicemail
boxes, federation hardening, and onboarding more exchanges.

## Support

If you'd like to support the project, see
[the funding file](.github/FUNDING.yml).
