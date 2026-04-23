# ADR-0007: JP Safe LoRa Profiles Are Allowlist-Based

## Status
Accepted

## Context
The Wio-SX1262 module has broad generic capability, but the Japan certification and ARIB rules narrow what should be used in production. `[S8][S16]`

A permissive configuration model would make it too easy to ship unsafe or non-repeatable settings.

## Decision
Production JP builds use an allowlist-based profile set:

- `JP125_LONG_SF10`
- `JP125_BAL_SF9`
- `JP250_FAST_SF8`
- `JP250_CTRL_SF9`

and do not expose unrestricted “global” radio configuration as a normal runtime path.

`SF11/SF12 @ 125kHz` are not part of the standard v1 JP production allowlist.

## Consequences
### Positive
- Safer default
- Easier field reproducibility
- Easier payload budgeting

### Negative
- Less flexibility for experiments
- Lab-only profiles must be explicitly separated from production
