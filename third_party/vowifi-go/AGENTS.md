# vowifi-go Agent Guide

This file gives Codex and other AI coding agents the project context needed to
work in this repository without rediscovering the same constraints each time.

## Project Summary

vowifi-go is an independent Go implementation of the VoHive VoWiFi runtime
boundary. It is intended to replace behavior that was previously provided by an
official closed-source VoWiFi library, while remaining usable by existing
VoHive versions through the public runtime APIs.

The implementation is still under active development and is not yet full
feature parity with the official closed-source implementation.

## Repository Boundaries

- Primary repository: this repository.
- Compatibility consumer: a sibling or otherwise configured VoHive checkout.
- Do not move implementation work back into `vohive` unless explicitly asked.
- Keep `vowifi-go` independently usable by old `vohive` checkouts.
- Use `vohive` only as a compatibility validator unless the user explicitly
  requests a `vohive` change.

## Main Package Map

- `engine/sim`: SIM AKA contracts.
- `engine/swu`: SWu/ePDG, IKEv2, EAP-AKA/AKA', ESP, MOBIKE, TUN, routing, and
  XFRM helpers.
- `runtimehost`: runtime lifecycle, modem/SIM boundaries, IMS registration,
  service wiring, and VoHive-facing APIs.
- `runtimehost/carrier`: carrier presets and policy overrides.
- `runtimehost/e911`: TS.43/E911 entitlement and challenge handling.
- `runtimehost/messaging`: IMS SMS, USSD, SMS PDU handling, segmentation, and
  inbound delivery helpers.
- `runtimehost/voiceclient`: SIP parsing, SIP transport, IMS registration
  primitives, security headers, and dialog request builders.
- `runtimehost/voicehost`: voice agents, SDP rewrite, RTP/RTCP relay, SRTP,
  inbound/outbound SIP interworking, and dialog operations.

## Development Priorities

- Prefer real protocol behavior over mock-only surface compatibility.
- Preserve existing public APIs used by VoHive unless a breaking change is
  explicitly approved.
- When adding protocol behavior, include focused tests for the exact wire/state
  transition being implemented.
- Keep modem, SIM, network, TUN, route, and command boundaries injectable so CI
  remains loopback-friendly.
- Treat operator-specific behavior as explicit compatibility work. Do not
  assume a carrier flow is complete without tests or real-device evidence.

## Validation Commands

Run the full local CI entry point before committing meaningful changes:

```sh
make ci
```

Useful focused commands:

```sh
go test ./...
go test ./runtimehost/voiceclient ./runtimehost/voicehost
go test ./runtimehost ./runtimehost/messaging ./runtimehost/e911
```

If Go is not on `PATH`, use:

```sh
GO=/path/to/go make ci
```

## VoHive Compatibility Check

After changes that affect public runtime behavior, also validate the old VoHive
consumer from a local VoHive checkout:

```sh
go test ./internal/vowifihost ./internal/device ./internal/api ./internal/notify ./internal/e911 -run 'VoWiFi|RuntimeStart|StartRuntime|Tunnel|MOBIKE|Dataplane|IMS|Voice|USSD|E911|Emergency|Websheet'
go build ./cmd/vohive
rm -f vohive
```

The VoHive checkout should remain clean unless the user explicitly requested a
change there.

## Documentation

- Keep `README.md` concise and English-first.
- Keep Chinese translations in `README.zh-CN.md` and
  `docs/DISCLAIMER.zh-CN.md`.
- Put implementation inventory in `docs/FEATURES.md`.
- Put package/runtime structure in `docs/ARCHITECTURE.md`.
- Put local and CI workflow notes in `docs/DEVELOPMENT.md`.
- Keep disclaimers in `docs/DISCLAIMER.md` and the Chinese translation.

## Git Rules

- Use the repository or user-configured Git identity.
- Do not hard-code personal emails, local absolute paths, or private development
  machine details into repository files, examples, generated docs, scripts, or
  commits.
- Push after committing when the user explicitly asks for push.
- Never revert unrelated local changes. If unrelated changes appear, leave them
  alone and mention them to the user.

## Current Known Gaps

Important remaining parity work includes:

- Real SIM/modem/operator validation matrix.
- Full IMS transaction timers and recovery semantics.
- P-CSCF failover and re-registration behavior across all voice, SMS, and USSD
  operations.
- Carrier-specific E911 and TS.43 behavior beyond the currently tested paths.
- Long-running SWu dataplane validation for TUN, routing, MTU, IPv4/IPv6, and
  NAT-T behavior.
- Real voice media interop for SDP variants, RTP/SRTP, RTCP feedback, DTMF,
  hold/resume, re-INVITE, UPDATE, and supplementary services.
