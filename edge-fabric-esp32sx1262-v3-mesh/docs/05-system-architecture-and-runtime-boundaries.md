# System Architecture And Runtime Boundaries

## 1. まず結論

この repo では system を次の 7 つに分けます。

1. **Node App**
2. **Node SDK**
3. **Mesh / Radio Runtime**
4. **Gateway Head / Mesh Root Runtime**
5. **Host Agent**
6. **Site Router**
7. **Client / Controller Services**

これを混ぜると、mesh の責務と業務ロジックが絡んで破綻します。

---

## 2. Planes

## 2.1 Application Plane
- 現場 UI
- automation
- analysis
- device-specific logic
- KGuard のような domain app

ここでは `state / event / command` を扱う。  
**bearer は扱わない。**

## 2.2 Fabric Control Plane
- identity
- lease
- role assignment
- route class policy
- parent hints
- preferred root / gateway set
- health aggregation

## 2.3 Fabric Data Plane
- message routing
- dedupe
- queue
- retry
- summary codec
- storage
- subscription fanout
- ack ownership

## 2.4 Radio Plane
- Wi-Fi mesh parent/child
- LoRa RX/TX
- relay queue
- CAD / RSSI / carrier sense helper
- neighbor table maintenance

---

## 3. Components

## 3.1 Node App
センサー読み取りや制御ロジックを書くところ。  
ここは「人感が反応した」「漏水した」「サーボ位置 120」などの意味を作る。

## 3.2 Node SDK
Node App に見せる API 層。  
例えば次のような API を想定する。

```cpp
fabric.publish_state("tank.level", buf, Priority::Normal);
fabric.emit_event("pir.triggered", buf, Priority::Critical);
fabric.accept_command("servo.set_angle", handler);
fabric.report_heartbeat(summary);
```

ここに `sendViaLora()` は出さない。

## 3.3 Mesh / Radio Runtime
Node 側の transport 実装です。  
責務:
- Wi-Fi mesh stack との接続
- LoRa overlay stack との接続
- local queue
- retry budget
- parent / relay selection
- immediate hop ack

## 3.4 Gateway Head / Mesh Root Runtime
always-on の上位ノードです。  
2 系統あります。

### A. gateway_head
USB で server にぶら下がる radio head。  
LoRa ingress/egress と一部 Wi-Fi ingress を担当。

### B. mesh_root
Wi-Fi mesh domain の root。  
domain の upstream 入口として振る舞う。 `[S20][S21]`

## 3.5 Host Agent
server 側の daemon です。  
USB gateway / mesh root / local root agent と話し、Site Router へ渡す。

## 3.6 Site Router
fabric の durable core です。  
責務:
- event ledger
- latest state projection
- command ledger
- queue / retry / DLQ
- dedupe
- route decision
- multi-host ingress merge
- multi-root / multi-gateway merge
- client pub/sub
- fabric summary

## 3.7 Client / Controller Services
Ubuntu server 上の制御アプリ、UI、ルールエンジンなどです。  
command を出してよいが、**node へ直送しない**。  
必ず Site Router を通る。

---

## 4. Wi-Fi mesh backbone, LoRa overlay, Fabric Spine

## 4.1 Wi-Fi mesh backbone
powered node が主骨格になる。  
ESP-WIFI-MESH は self-organizing / self-healing で、root を中心に親子関係を形成する。 `[S20][S21]`

この repo では、次を採用する。
- powered node は `wifi_mesh` を first-class に扱う
- mesh router / mesh root role を定義する
- command / high-rate telemetry / large state はまず Wi-Fi に寄せる

## 4.2 LoRa sparse mesh overlay
SX1262 を使う長距離 overlay。  
これは LoRaWAN ではなく raw LoRa の custom mesh です。

この repo では、次を採用する。
- sleepy leaf は direct uplink or bounded relay
- relay は powered node only
- hop cap は保守的に管理
- summary / alert / small control に絞る
- bulk は載せない

## 4.3 Fabric Spine
複数 root / gateway / server を束ねる上位ネットワーク。  
例:
- USB CDC
- Ethernet
- local Wi-Fi IP
- VLAN
- VPN

Wi-Fi mesh の root 同士が no-router で直接会話できる前提にはしない。  
必要なら Fabric Spine か dedicated bridge でつなぐ。 `[S22][S21]`

---

## 5. Multiple domains and multiple servers

### 5.1 1 site = 1 logical writer
server が 2 台あっても、logical writer は 1 つに保つ。

### 5.2 ingress は増やしてよい
- USB gateway 2 台
- mesh root 3 台
- LoRa relay bridge 2 台
は可能。

### 5.3 controller は複数よい
- operator UI
- automation
- analytics
- maintenance app
が同時接続してよい。

### 5.4 mesh domain は複数よい
building ごとに別 mesh domain を持ってよい。  
ただし domain 間通信は Site Router / Fabric Spine / bridge で束ねる。

---

## 6. Runtime ownership

| Layer | Owns | Must not own |
|---|---|---|
| Node App | sensor / actuator meaning | radio routing |
| Node SDK | message API | durable site ledger |
| Radio Runtime | bearer handling / local queue | business logic |
| Gateway Head | ingress / egress | durable writer |
| Host Agent | device/session bridge | site-level semantics |
| Site Router | ledger / dedupe / routing | GPIO / radio IRQ |
| Client Service | business flows | direct node transport |

---

## 7. Why this split matters

例えば「人が入ったらすぐ通知して、遠くの server にも残したい」という要求でも、
- PIR の edge detection は local
- local buzzer は local
- Wi-Fi mesh で近い controller へ低遅延通知
- LoRa summary で遠距離 fallback
- Site Router が履歴と dedupe を担当
という役割分担ができる。

これを 1 つの firmware に押し込むと、mesh と業務ロジックが分離できなくなる。
