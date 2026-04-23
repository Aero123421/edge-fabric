# Codex Implementation Brief And Repo Map

## 1. 大前提

この repo は、**一気に全部実装するための仕様**ではなく、  
**順番に失敗しにくく実装するための仕様**です。

mesh-first にしたことで、最終形は広くなりました。  
だからこそ、Codex には次の順序で着手させるべきです。

---

## 2. 実装の順番

## Phase 0: contracts and storage core
最初に作るもの:
- envelope model
- manifest / lease
- event ledger
- latest state projection
- command ledger
- dedupe store
- queue / DLQ
- local API

理由:
- mesh の前に durable core が必要
- logical writer を先に固めないと後で壊れる

## Phase 1: single gateway / direct star
次に作るもの:
- gateway_head USB link
- Host Agent
- Site Router ingest
- sleepy_leaf direct LoRa uplink
- powered_leaf Wi-Fi IP direct
- basic ack / retry / heartbeat

理由:
- radio / host / writer を end-to-end でまず通す

## Phase 2: Wi-Fi mesh backbone
次に作るもの:
- mesh_root runtime
- mesh_router runtime
- mesh event bridge
- parent / child health
- route class = immediate_local / normal_state

理由:
- powered backbone が fabric の主骨格だから

## Phase 3: LoRa sparse relay
次に作るもの:
- lora_relay runtime
- relay beacon
- neighbor table
- hop ack
- hop limit / duplicate cache
- direct vs 1-relay selection

理由:
- long-range overlay の基本形を作る

## Phase 4: hybrid routing
次に作るもの:
- route cost model
- hysteresis
- summary codec
- redundant critical alert
- over-cap handling

理由:
- ここで初めて “LoRa と Wi-Fi を勝手に選ぶ” 感覚が出る

## Phase 5: multi-domain / multi-host / multi-controller
次に作るもの:
- multi-ingress merge
- domain IDs
- gateway / root arbitration
- targeted host/client routing
- controller subscriptions

理由:
- user の「Ubuntu server 複数」「色んな方式で管理したい」に答える段階

---

## 3. Repo map

### firmware/node-sdk
- app-facing API
- local queue
- route class selection entrypoint
- sleepy lifecycle helpers
- maintenance_awake helpers

### firmware/gateway-head
- SX1262 HAL wrapper
- USB CDC protocol
- LoRa RX/TX
- minimal local ack
- health export

### firmware/mesh-root
- ESP-WIFI-MESH integration
- root uplink/downlink bridge
- toDS / fromDS bridge
- root health export
- optional local LoRa bridge hooks

### firmware/mesh-router
- ESP-WIFI-MESH parent/child runtime
- local forwarding telemetry
- optional dual-bearer bridge helper

### firmware/lora-relay
- LoRa beacon runtime
- relay queue
- duplicate cache
- hop ack
- route hint broadcast

### host/agent
- USB enumeration
- root / gateway sessions
- short-term spool
- Site Router uplink
- diagnostics

### host/site-router
- writer
- dedupe
- queue
- lease
- route decision
- client API
- metrics

### sdk/client
- pub/sub API
- command API
- query API
- route-insensitive app SDK

---

## 4. Suggested first classes / modules

### on node side
- `FabricEnvelope`
- `RouteClass`
- `NodeQueue`
- `SleepyPolicy`
- `SummaryCodecRegistry`
- `RadioRuntime`
- `LeaseState`
- `NeighborCache`

### on root / relay side
- `MeshDomainRuntime`
- `LoRaRelayRuntime`
- `IngressMetadata`
- `AckTracker`
- `RouteDecision`
- `DuplicateCache`

### on host side
- `GatewaySession`
- `RootSession`
- `IngressMerger`
- `SiteLedger`
- `StateProjector`
- `CommandService`
- `LeaseService`
- `FabricSummaryService`

---

## 5. What not to code first

- full multi-master Site Router
- generic LoRa fragmentation
- OTA over LoRa
- fully automatic free-form routing
- deep sleep relay
- controller-specific business logic
- UI polish

---

## 6. What to lock before coding

- message IDs
- role names
- route class names
- payload cap policy
- sleepy leaf prohibition set
- relay hop cap
- summary codec naming
- ack phases
- ingress metadata fields
- manifest / lease schema fields

---

## 7. First acceptance demo

Codex の最初のデモとしては、次の構成がベストです。

- Ubuntu server 1
- gateway_head 1
- mesh_root 1
- mesh_router 1
- powered_leaf 2
- sleepy_leaf 2
- lora_relay 1

デモ内容:
1. powered leaf の command は Wi-Fi mesh で低遅延
2. sleepy leaf の event は LoRa direct or 1-relay
3. same critical event を Wi-Fi + LoRa redundant send
4. Site Router が dedupe
5. relay failure で degraded fallback
6. sleepy leaf は relay role を取らない

これが通れば、この repo の骨格はかなり正しい。
