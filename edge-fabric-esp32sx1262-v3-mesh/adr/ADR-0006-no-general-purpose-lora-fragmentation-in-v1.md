# ADR-0006: No General-Purpose LoRa Fragmentation In V1

## Status
Accepted

## Context
SX126x family capabilities and related Semtech material show that large packets are technically possible, but v1 of this fabric is constrained by:

- single-channel head occupancy
- JP-safe airtime limits
- sleepy node wake budgets
- compact control/summary-first philosophy

General-purpose fragmentation would require fragment IDs, reassembly timers, duplicate handling, missing fragment recovery, and complex failure semantics.

## Decision
V1 keeps a reserved fragment frame family, but:

- general-purpose LoRa fragmentation is disabled by default
- `bulk` never falls back to LoRa
- only messages with an explicit compact LoRa codec may use LoRa
- everything else goes to Wi-Fi, queue, or reject path

## Consequences
### Positive
- Much simpler implementation
- Predictable LoRa airtime
- Fewer sleepy node edge cases

### Negative
- Some large messages will need explicit summary codecs
- Site designers must rely on Wi-Fi or maintenance mode for rich diagnostics
