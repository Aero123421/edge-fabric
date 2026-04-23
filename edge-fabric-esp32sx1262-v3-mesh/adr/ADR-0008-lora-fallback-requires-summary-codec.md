# ADR-0008: LoRa Fallback Requires An Explicit Summary Codec

## Status
Accepted

## Context
A “hybrid LoRa/Wi-Fi” fabric only works if bearer choice does not leak into application code, but payload shape still matters.

Without explicit summary codecs, payload-over situations become ambiguous and lead to ad hoc truncation or implicit fragmentation.

## Decision
Every message type must declare one of:

- Wi-Fi full only
- LoRa native
- dual-shape (Wi-Fi full + LoRa summary)
- compact control

LoRa fallback is allowed only when an explicit LoRa-compatible codec exists.

## Consequences
### Positive
- No hidden truncation
- Clear over-cap policy
- Cleaner application/API separation

### Negative
- More up-front codec design work
- Some app message types need both full and summary representations
