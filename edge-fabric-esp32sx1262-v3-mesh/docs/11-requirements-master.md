# Requirements Master

## 1. 記法

- **MUST**: v1 で必須
- **SHOULD**: 強く推奨
- **MAY**: 拡張で許可

---

## 2. Product goals

### GOAL-001
system は、ESP32-S3 + SX1262 を使う node 群を **1 つの fabric** として扱えること。

### GOAL-002
system は、**Wi-Fi mesh backbone + LoRa sparse mesh overlay** を統合して扱えること。

### GOAL-003
アプリ開発者は bearer 名を知らなくても `state / event / command / heartbeat / file_chunk` を扱えること。

### GOAL-004
site は 1 gateway / 1 server から始めて、複数 root / 複数 gateway / 複数 host / 複数 controller client にスケールできること。

### GOAL-005
battery sleepy leaf と powered always-on node を同一 fabric に混在させられること。

### GOAL-006
JP production 運用では、region / power / antenna / airtime policy を fabric 側で制御できること。

---

## 3. Hardware and platform requirements

### HW-001
v1 reference hardware は XIAO ESP32S3 + Wio-SX1262 kit とすること。

### HW-002
Node firmware / Mesh Root firmware / Gateway Head firmware は ESP-IDF を primary support にすること。

### HW-003
GPIO7/8/9 と GPIO38/39/40/41/42 は Radio HAL 所有とし、app layer に直接公開しないこと。

### HW-004
D0〜D5, D6, D7 は app-facing GPIO として first-class support すること。

### HW-005
gateway_head の host link は USB CDC を mandatory path とすること。

### HW-006
gateway_head は sleep しないこと。

### HW-007
mesh_router / mesh_root / lora_relay は always-on power を前提にすること。

### HW-008
sleepy leaf に relay duty を割り当てないこと。

---

## 4. Architectural requirements

### ARC-001
system は Node SDK, Radio Runtime, Gateway/Root Runtime, Host Agent, Site Router, Client API に責務分離すること。

### ARC-002
logical writer は Site Router とすること。

### ARC-003
Gateway Head / Mesh Root は durable site ledger を持たないこと。

### ARC-004
controller client は Site Router 経由で command を送ること。

### ARC-005
site は local-first であり、cloud は optional adapter とすること。

### ARC-006
same site で複数の logical writer が同時 active になる標準構成を採用しないこと。

### ARC-007
multiple Host Agent ingress を許容すること。

### ARC-008
multiple Gateway Head / Mesh Root ingress を許容すること。

### ARC-009
same message が複数 ingress から到着しても dedupe できること。

### ARC-010
project-specific business logic を core から分離できること。

---

## 5. Role and power requirements

### ROLE-001
node は少なくとも `network_role`, `power_class`, `wake_class`, `supported_bearers` で分類されること。

### ROLE-002
v1 で少なくとも以下の role を持つこと。  
`sleepy_leaf`, `powered_leaf`, `mesh_router`, `mesh_root`, `lora_relay`, `dual_bearer_bridge`, `gateway_head`, `site_router`, `controller_client`

### ROLE-003
`sleepy_leaf MUST NOT relay`

### ROLE-004
`sleepy_leaf MUST NOT become mesh_router or mesh_root`

### ROLE-005
`lora_relay MUST be always_on and powered`

### ROLE-006
`mesh_router MUST be always_on and powered`

### ROLE-007
`dual_bearer_bridge SHOULD be powered_leaf superset role` として扱えること。

### ROLE-008
role は boot 時 capability と lease により確定できること。

### ROLE-009
site policy は role を上書きできるが、power class 制約に反する role を配布してはならないこと。

---

## 6. Wi-Fi mesh requirements

### WM-001
powered backbone には ESP-WIFI-MESH を first-class choice として扱うこと。 `[S20][S21]`

### WM-002
system は Wi-Fi mesh domain を識別する `mesh_domain_id` を持つこと。

### WM-003
mesh_root role は upstream IP / host connection を持てること。

### WM-004
mesh_router role は parent / child forwarding を担えること。

### WM-005
system は parent connected / child connected / root switched / lost IP 等の mesh event を fabric health に反映できること。

### WM-006
powered node の primary route は原則 Wi-Fi mesh / Wi-Fi IP とすること。

### WM-007
system は複数 Wi-Fi mesh domain を同一 site に許容すること。

### WM-008
multiple root no-router の直接相互通信を前提にしないこと。domain 間は Site Router / Fabric Spine / bridge で束ねること。 `[S22][S21]`

### WM-009
Wi-Fi mesh root の upstream queue 停滞を監視できること。

### WM-010
Wi-Fi mesh で運ぶ payload は framework 独自の soft cap を持つこと。過大 payload は chunk 化すること。

### WM-011
Wi-Fi mesh route failure 時、parent reselection の hold-down と hysteresis を持つこと。

### WM-012
mesh_router / mesh_root の route health を heartbeat に反映すること。

---

## 7. LoRa mesh requirements

### LM-001
LoRa bearer は raw LoRa custom overlay とし、LoRaWAN clone を前提にしないこと。

### LM-002
LoRa overlay は `direct`, `1-relay`, `2-relay` を標準サポートとすること。

### LM-003
`3-relay` 以上は experimental 扱いとし、v1 production default にしないこと。

### LM-004
relay duty は `lora_relay` または `dual_bearer_bridge` role のみが担えること。

### LM-005
sleepy leaf は relay beacon の常時受信を前提にしないこと。

### LM-006
LoRa relay は neighbor beacon / hello / route hint を発行できること。

### LM-007
LoRa relay は recent message-id cache による loop suppression / duplicate suppression を持つこと。

### LM-008
LoRa relay packet は hop limit を持つこと。

### LM-009
LoRa overlay で運ぶ payload class は summary / sparse / compact control に限定すること。

### LM-010
bulk / OTA / file / verbose log は LoRa overlay に載せないこと。

### LM-011
JP production build では RegionPolicy::JP を mandatory とすること。

### LM-012
JP production build では profile allowlist を使うこと。

### LM-013
LoRa TX 前に JP policy に必要な clear-channel / carrier-sense policy を適用できること。 `[S16][S18]`

### LM-014
LoRa route 選択は airtime / hop count / relay load / link health / duty headroom を考慮できること。

### LM-015
LoRa relay は queue overflow 時に `bulk` ではなく lower-priority summary を先に落とせること。

### LM-016
LoRa overlay の end-to-end ack owner を決められること。

### LM-017
LoRa fallback は summary codec が存在する message class に限定すること。

### LM-018
`critical alert` は policy により redundant Wi-Fi + LoRa 送信を許可してよいこと。

---

## 8. Hybrid routing requirements

### HYB-001
route decision は bearer 名ではなく `route_class` を主語に行うこと。

### HYB-002
少なくとも以下の route class を定義すること。  
`immediate_local`, `normal_state`, `critical_alert`, `sparse_summary`, `bulk_only`, `maintenance_sync`

### HYB-003
powered node の default primary は Wi-Fi 側とすること。

### HYB-004
sleepy leaf の default primary は LoRa とすること。

### HYB-005
same event の redundant send 時、`event_id` は同一であること。

### HYB-006
route change には hysteresis を持たせること。

### HYB-007
link flap 時に packet ごとに bearer が激しく踊らないこと。

### HYB-008
over-cap payload では summary codec or Wi-Fi defer を行い、silent drop しないこと。

### HYB-009
immediate_local class は LoRa へ逃がさないこと。

### HYB-010
bulk_only class は LoRa へ逃がさないこと。

### HYB-011
critical_alert class は policy により LoRa direct / relay / redundant を選べること。

### HYB-012
maintenance_sync class は maintenance_awake node でのみ許可されること。

---

## 9. Sleepy node requirements

### SL-001
sleepy node は deep sleep を主体にし、接続維持型 Wi-Fi node として扱わないこと。 `[S15]`

### SL-002
sleepy node の command service level は `eventual / next-poll` を標準とすること。

### SL-003
sleepy node は次回 wake まで command 未達である可能性を API 契約に含めること。

### SL-004
sleepy node は maintenance_awake mode を持てること。

### SL-005
sleepy node は last-good relay/root list を永続化できること。

### SL-006
sleepy node は wake 時に bounded scan だけを行い、aggressive roaming をしないこと。

### SL-007
sleepy node の heartbeat は piggyback を優先すること。

### SL-008
sleepy node は downlink receive window を bounded で持つこと。

### SL-009
sleepy node は relay/backbone duty を引き受けないこと。

---

## 10. Message and contract requirements

### MSG-001
logical message kind として少なくとも `state`, `event`, `command`, `command_result`, `heartbeat`, `manifest`, `lease`, `file_chunk`, `fabric_summary` を定義すること。

### MSG-002
all messages は `schema_version`, `message_id`, `priority`, `kind`, `source`, `target` を持つこと。

### MSG-003
`event` は `event_id` を持てること。

### MSG-004
`command` と `command_result` は `command_id` を持つこと。

### MSG-005
source は `hardware_id`, `session_id`, `seq_local`, optional `fabric_short_id` を持てること。

### MSG-006
target は `node`, `group`, `service`, `host`, `client`, `site`, `broadcast` を表現できること。

### MSG-007
message は optional `mesh_meta` を持てること。  
例:
- `mesh_domain_id`
- `route_class`
- `hop_limit`
- `hop_count`
- `last_hop`
- `ingress_gateway_id`
- `relay_trace`

### MSG-008
payload serialization は logical schema と on-wire binary encoding を分離すること。

### MSG-009
Wi-Fi full-shape と LoRa summary-shape を別定義できること。

### MSG-010
compact LoRa envelope は direct と relayed で overhead が異なってよいこと。

---

## 11. Ordering, idempotency, and duplicate handling

### ORD-001
ordering 正本は `occurred_at`, `session_id`, `seq_local` とすること。

### ORD-002
same real-world event の retry では `event_id` を変えないこと。

### ORD-003
same operator action の retry では `command_id` を変えないこと。

### ORD-004
same `session_id` 内で `seq_local` は単調増加すること。

### ORD-005
relay / multi-ingress duplicate を Site Router で吸収できること。

### ORD-006
relay loop 防止のため、LoRa message は hop limit と recent-id cache を使うこと。

---

## 12. Addressing and routing requirements

### ROUTE-001
node app は通常 `service` / `group` / `node` addressing を使うこと。

### ROUTE-002
PC / Ubuntu server などの topology は node に直接露出しないこと。

### ROUTE-003
Site Router は `host` と `client` 宛の targeted routing を提供すること。

### ROUTE-004
Site Router は `service` 宛 pub/sub fanout を提供すること。

### ROUTE-005
Site Router は ingress metadata を保持すること。  
例: `gateway_id`, `mesh_domain_id`, `rssi`, `snr`, `rx_time`, `bearer`

### ROUTE-006
downlink gateway / root / bridge 選択ロジックを持つこと。

### ROUTE-007
route decision は last-good metrics, link health, wake class, hop count, payload size を見られること。

### ROUTE-008
domain 間通信は Site Router / Fabric Spine / bridge で成立させること。

---

## 13. ACK and QoS requirements

### QOS-001
system は hop ack と end-to-end persist ack を区別できること。

### QOS-002
system は command phase ack を区別できること。  
`issued -> accepted -> executing -> succeeded / failed / rejected / expired`

### QOS-003
priority は少なくとも `critical`, `control`, `normal`, `bulk` を持つこと。

### QOS-004
`critical` と `control` は durable queue を持つこと。

### QOS-005
`bulk` は resumable chunking を前提にすること。

### QOS-006
same `event_id` の duplicate delivery を吸収できること。

### QOS-007
mesh root / relay failure 時、pending data の扱いが定義されること。

### QOS-008
LoRa relay hop ack は bounded short ack とすること。

### QOS-009
sleepy leaf に対する end-to-end ack は next receive window / next poll で返りうること。

---

## 14. Heartbeat and health requirements

### HB-001
Node heartbeat を定義すること。

### HB-002
Gateway heartbeat を定義すること。

### HB-003
Mesh root heartbeat を定義すること。

### HB-004
Host Agent heartbeat を定義すること。

### HB-005
Fabric summary heartbeat を定義すること。

### HB-006
heartbeat には link / queue / battery / reset cause / wake class / route health を含められること。

### HB-007
sleepy node では heartbeat piggyback を許可すること。

### HB-008
mesh router / root heartbeat は child count / parent state / toDS backlog を含められること。

### HB-009
lora_relay heartbeat は duty headroom / queue depth / beacon age を含められること。

---

## 15. Storage and queue requirements

### STORE-001
Site Router は event ledger, latest state projection, command ledger, DLQ を持つこと。

### STORE-002
Host Agent は短期 spool を持てること。

### STORE-003
relay / root / gateway は queue overflow を観測可能にすること。

### STORE-004
node 側 durable queue は priority-aware であること。

### STORE-005
flash wear を壊さない coalescing / batching / dirty-bit policy を持つこと。

### STORE-006
sleepy leaf は wake ごとに全ての統計をフラッシュへ書かないこと。

---

## 16. Security and provisioning requirements

### SEC-001
hardware_id は immutable identity とすること。

### SEC-002
logical_binding_id は site 内で mutable binding とすること。

### SEC-003
fabric_short_id は compact on-wire use のために lease で配布できること。

### SEC-004
join / claim / role lease は separate control path で処理すること。

### SEC-005
QR に long-term secret を直書きしないこと。

### SEC-006
on-wire payload 保護は bearer 上ではなく fabric layer で実施できること。

---

## 17. Capacity and scale requirements

### SCALE-001
v1 は single site / single logical writer を前提にすること。

### SCALE-002
site 全体として数十〜数百 device を扱える設計指針を持つこと。

### SCALE-003
その際、LoRa は sparse edge traffic に限定し、site 全 traffic の主骨格を担わせないこと。

### SCALE-004
Wi-Fi mesh domain は building / floor / zone 分割を前提にできること。

### SCALE-005
Fabric Spine は multiple roots / gateways / servers を吸収できること。

### SCALE-006
scale guidance は traffic profile 依存であることを docs に明記すること。

---

## 18. Validation requirements

### VAL-001
sleepy leaf が relay role を誤って受け取れないことを test すること。

### VAL-002
Wi-Fi mesh root failure で parent reselection / root switch の挙動を観測すること。

### VAL-003
LoRa relay failure で route reselection / retry degradation を観測すること。

### VAL-004
same event の redundant Wi-Fi + LoRa delivery で dedupe が成立すること。

### VAL-005
over-cap payload が summary codec or defer へ落ちること。

### VAL-006
multi-host / multi-gateway ingest でも logical writer が 1 つに保たれること。

### VAL-007
LoRa relay hop count に応じて effective occupancy が増えることを計測可能にすること。

---

## 19. Explicit “must nots”

### X-001
deep sleep node を relay にしない。

### X-002
battery node を backbone にしない。

### X-003
bulk / OTA / image を LoRa mesh に載せない。

### X-004
LoRa mesh に closed-loop servo 制御を載せない。

### X-005
multiple no-router roots が直接会話できる前提にしない。

### X-006
packet 単位の無制限な自由 route 変更をしない。

### X-007
Gateway Head に durable site state を持たせない。
