# Epics And First Slices

## Epic 1: Contracts and identity
- envelope v1
- manifest v1
- lease v1
- short-id lease
- route_class enum
- role/power/wake enums

## Epic 2: Site Router core
- event ledger
- latest state projection
- command ledger
- dedupe
- queue / DLQ
- client pub/sub

## Epic 3: Direct transports
- gateway_head USB protocol
- Host Agent sessioning
- sleepy_leaf direct LoRa
- powered_leaf Wi-Fi IP

## Epic 4: Wi-Fi mesh backbone
- mesh_root runtime
- mesh_router runtime
- root health export
- domain health model
- immediate_local path

## Epic 5: LoRa sparse relay
- relay beacon
- neighbor table
- 1-relay forwarding
- hop ack
- duplicate cache
- hop limit enforcement

## Epic 6: Hybrid route engine
- route cost model
- hysteresis
- summary codec registry
- over-cap policy
- redundant critical path

## Epic 7: Multi-domain / multi-host
- domain ids
- multi-ingress merge
- gateway/root arbitration
- host/client targeted routing

## Epic 8: Ops and validation
- metrics
- health summary
- failure injection
- power regression checks
- payload fit tests

## First slice suggestion
1. Contracts
2. Site Router core
3. Direct sleepy LoRa + powered Wi-Fi
4. Wi-Fi mesh backbone
5. LoRa relay
6. Hybrid routing
