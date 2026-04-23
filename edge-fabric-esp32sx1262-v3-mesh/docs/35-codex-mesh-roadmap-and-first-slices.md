# Codex Mesh Roadmap And First Slices

## 1. いきなり全部作らない

mesh-first 版は広いので、Codex には slice ごとに渡す必要があります。

---

## 2. Slice plan

## Slice 1: core contracts
- envelope
- manifest
- lease
- event_id / command_id
- route_class enum
- power/wake/network role enums

### done when
JSON schema と fixture が通る。

## Slice 2: direct fabrics
- sleepy_leaf direct LoRa uplink
- powered_leaf Wi-Fi direct
- Site Router ingest
- persist ack
- queue / dedupe

### done when
single Ubuntu + 1 gateway + 2 leaf types が動く。

## Slice 3: Wi-Fi mesh backbone
- mesh_root
- mesh_router
- root to Site Router bridge
- domain health
- immediate_local path

### done when
powered node command が mesh 経由で安定。

## Slice 4: LoRa relay
- relay beacon
- neighbor table
- 1-relay forwarding
- hop ack
- relayed body size checks

### done when
sleepy leaf -> relay -> gateway -> Site Router が通る。

## Slice 5: hybrid route engine
- route class resolver
- cost model
- hysteresis
- summary codec registry
- redundant critical path

### done when
Wi-Fi and LoRa are selected by policy, not by app code.

## Slice 6: multi-domain / multi-host
- domain ids
- multi-ingress merge
- root/gateway arbitration
- host/client addressing

### done when
2 Ubuntu servers + multiple roots/gateways が coherent に動く。

---

## 3. Concrete first demo backlog

### firmware/node-sdk
- basic publish/command API
- sleepy cycle helper
- summary codec hook

### firmware/gateway-head
- USB framing
- SX1262 send/recv
- ingress metadata export

### firmware/mesh-root
- root state machine
- toDS bridge
- root health packets

### firmware/lora-relay
- small beacon packet
- relay queue
- recent message cache
- hop limit check

### host/site-router
- event ledger
- dedupe
- route decision stub
- controller pub/sub

---

## 4. “Do not let Codex do this first”

- multi-master replication
- LoRa fragmentation
- OTA over LoRa
- generalized free routing
- battery relay
- dozens of message schemas before core envelope stabilizes

---

## 5. Definition of success for v1

v1 が成功と言えるのは、少なくとも次が揃った時。

1. powered Wi-Fi mesh backbone works
2. sleepy LoRa leaves work
3. LoRa relay works in bounded way
4. deep sleep nodes never relay
5. payload over-cap is handled sanely
6. multiple ingress points do not break state correctness
7. app code never directly chooses bearer
