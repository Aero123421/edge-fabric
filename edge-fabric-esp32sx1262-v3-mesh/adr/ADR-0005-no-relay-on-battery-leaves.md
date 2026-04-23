# ADR-0005: No Relay On Battery Leaves

## Status
Accepted

## Context
Battery-powered sleepy nodes are the primary target for long-life sensor roles.  
Relay / bridge behavior requires additional listen time and often additional synchronization complexity.

Semtech's relay white paper explains that the main energy cost of relay behavior is listening for channel activity, and also notes that adding relay functionality to a sensor device makes battery-life prediction challenging. `[S17]`

## Decision
The core fabric defines:

- `battery_leaf MUST NOT relay`
- `battery_leaf MUST NOT backbone`
- `battery_leaf MUST NOT be generic store-and-forward node`

`dual_stack_bridge` and any relay-like role are powered-only.

## Consequences
### Positive
- Battery model stays predictable
- Sleepy node command model remains simple
- No hidden always-listening behavior in leaf nodes

### Negative
- Some edge coverage problems must be solved with powered bridge/gateway placement
- Battery-only mesh dreams are explicitly out of scope for v1
