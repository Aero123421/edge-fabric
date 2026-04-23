# Node Roles, Mesh Topologies, And Transport Policy

## 1. role を細かく分ける理由

mesh-first 設計では、「ESP32-S3 + SX1262 なら全部同じ」では足りません。  
同じ hardware でも、

- battery で deep sleep する leaf
- always-on の powered router
- LoRa relay bridge
- Wi-Fi mesh root
- USB gateway head

では、許される責務がまったく違います。

---

## 2. Standard network roles

## 2.1 sleepy_leaf
- power: battery / harvested
- wake: `sleepy_periodic` or `sleepy_event`
- primary: `lora`
- optional maintenance: `wifi_ip` or `ble`
- 用途: sparse telemetry, alert, summary heartbeat
- 禁止: relay, backbone, continuous parent duties

## 2.2 powered_leaf
- power: mains / usb
- wake: `always_on`
- primary: `wifi_mesh` or `wifi_ip`
- secondary: optional `lora`
- 用途: detailed telemetry, command target, actuator node
- 禁止: site writer

## 2.3 mesh_router
- power: mains / usb
- wake: `always_on`
- primary: `wifi_mesh`
- 用途: parent/child forwarding, local backbone
- optional: edge compute, local aggregation
- 禁止: sleepy behavior

## 2.4 mesh_root
- power: mains / usb
- wake: `always_on`
- primary: `wifi_mesh`
- upstream: `wifi_ip`, `ethernet`, `usb`, `4g` など site-specific
- 用途: domain root, upstream出口
- 禁止: durable site writer

## 2.5 lora_relay
- power: mains / usb
- wake: `always_on`
- primary: `lora`
- optional secondary: `wifi_mesh` / `wifi_ip`
- 用途: sparse summary relay, long-range hop extension
- 禁止: bulk carrier, sleepy role

## 2.6 dual_bearer_bridge
- power: mains / usb
- wake: `always_on`
- primary: `wifi_mesh + lora`
- 用途: mesh domain と LoRa overlay の境界点
- 備考: v1 で very important role

## 2.7 gateway_head
- power: usb
- wake: `always_on`
- primary: `lora` (+ optional local Wi-Fi)
- host link: `usb_cdc`
- 用途: radio head
- 禁止: durable writer, business logic

## 2.8 site_router
- power: mains / server
- wake: `always_on`
- primary: local IP / storage
- 用途: logical writer, durable core

## 2.9 controller_client
- power: server / PC
- wake: `always_on`
- 用途: UI, automation, analysis, control apps

---

## 3. Mandatory prohibitions

### 3.1 sleepy_leaf MUST NOT relay
最重要ルール。

### 3.2 sleepy_leaf MUST NOT become mesh_router or mesh_root
Wi-Fi connection maintenance と parent duty に向かない。 `[S15]`

### 3.3 lora_relay MUST be powered and always_on
listen し続ける必要があるから。

### 3.4 actuator closed-loop MUST stay local
servo / motor / safety control は local controller で完結させる。

### 3.5 gateway_head MUST NOT become durable site writer
site state は server 側で確定する。

---

## 4. Topology archetypes

## 4.1 direct star
- 1 gateway head
- LoRa direct leaves
- Wi-Fi powered leaves
- no relay

最小構成。まずここから始められる。

## 4.2 Wi-Fi mesh backbone + LoRa edge leaves
- mesh root 1
- mesh router 複数
- sleepy LoRa leaves 複数
- dual_bearer_bridge 1〜2

最もバランスが良い標準構成。

## 4.3 multi-domain site
- building A に mesh root A
- building B に mesh root B
- building 間は Fabric Spine or LoRa bridge
- Site Router が 1 logical writer

複数 server / 複数 controller に伸ばしやすい。

## 4.4 sparse long-range overlay
- edge の battery leaves
- powered LoRa relay 1〜2 hop
- backbone 側は Wi-Fi mesh

遠い屋外ノードや別棟向け。

## 4.5 control island
- local control node + servo
- Wi-Fi mesh で上位と接続
- LoRa は summary / fallback のみ

“サーボを fabric で扱う” と言っても、閉ループ制御はここに寄せる。

---

## 5. Transport policy by role

| Role | Primary | Secondary | Typical use | Notes |
|---|---|---|---|---|
| sleepy_leaf | LoRa | maintenance Wi-Fi | alert / sparse telemetry | deep sleep / no relay |
| powered_leaf | Wi-Fi mesh/IP | LoRa summary | telemetry / control target | immediate command 可 |
| mesh_router | Wi-Fi mesh | none / optional LoRa | forwarding backbone | self-healing parent/child |
| mesh_root | Wi-Fi mesh + upstream IP | optional LoRa | domain egress | root switching / upstream |
| lora_relay | LoRa | optional Wi-Fi | sparse relay | hop-budget 管理 |
| dual_bearer_bridge | Wi-Fi mesh + LoRa | IP | island bridge | key role |
| gateway_head | LoRa + USB | local Wi-Fi optional | radio ingress | host-managed |
| site_router | local IP/storage | N/A | writer / queue / dedupe | site core |

---

## 6. What “automatic optimization” means here

### 許可する
- powered node が Wi-Fi mesh を優先
- Wi-Fi 不調時に LoRa summary fallback
- critical event の redundant send
- relay parent の bounded reselection
- root / gateway の last-good 選択
- over-cap 時の summary codec 適用

### 許可しない
- packet ごとに自由な bearer dance
- sleepy node の aggressive scan
- relay を battery に押し付けること
- large payload を LoRa fragmentation で無理やり送ること
- actuator closed-loop を LoRa へ逃がすこと

---

## 7. Wi-Fi mesh policy

ESP-WIFI-MESH は self-organizing / self-healing で、root と parent/child を持つ。 `[S20][S21]`  
この repo では powered backbone にこれを使う。

### 7.1 standard rule
- powered always-on nodes SHOULD support `wifi_mesh`
- `mesh_router` / `mesh_root` roles are first-class
- ルータや IP に直接届く node が root 候補になる

### 7.2 practical rule
- v1 の recommended depth は浅めに保つ
- building / floor / zone 単位で domain を分ける
- root を増やすときは domain を分ける
- root 間は Fabric Spine で束ねる

### 7.3 no-router caveat
ESP FAQ では、no-router scenario で複数 root 同士は送信できないとされる。 `[S22]`  
そのため、multi-domain はアプリ層 / spine 層で束ねる前提にする。

---

## 8. LoRa mesh policy

LoRa 側は “何でも mesh” にしない。

### 8.1 direct first
基本は direct uplink。

### 8.2 relay only where needed
届かない edge だけ relay を使う。

### 8.3 hop cap
- default: max 2 relay hop まで
- 3 hop は experimental
- sleepy leaf の regular profile は direct または 1 relay を優先

### 8.4 relay payload class
relay で運んでよいのは、
- alert summary
- sparse state
- compact command stub
- compact command result
- compact heartbeat
だけ。

---

## 9. Scaling rule of thumb

- **数十台**: direct + Wi-Fi mesh backbone でかなりいける
- **100台規模**: domain 分割と route policy が必要
- **数百台**: 全体では可能だが、LoRa は sparse edge に限定し、Wi-Fi/IP backbone を主体にする

つまり、“数百台全部が LoRa mesh で chatty” は狙わない。  
“site 全体として数百台を fabric で扱える” を狙う。
