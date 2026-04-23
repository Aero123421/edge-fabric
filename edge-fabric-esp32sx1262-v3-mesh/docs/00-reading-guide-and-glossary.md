# Reading Guide And Glossary

この repo は、**ESP32-S3 + SX1262 向けの mesh-first hybrid fabric** の仕様です。  
用語が多いので、最初に読む人向けに意味を固定します。

## 1. まず全体像

この repo では、ネットワーク全体を 3 層で考えます。

### 1.1 Wi-Fi Mesh Backbone
always-on の powered node が作る主骨格です。  
ESP-WIFI-MESH を使い、**自立的に親を選び、壊れたら再接続し、root を持つ**ネットワークです。 `[S20][S21]`

### 1.2 LoRa Sparse Mesh Overlay
長距離・低頻度・低消費向けの疎なメッシュです。  
raw LoRa を使った **custom store-and-forward overlay** であり、LoRaWAN そのものではありません。  
relay は always-on の powered node に限定します。

### 1.3 Fabric Spine
mesh domain 同士、gateway、server、controller client をつなぐ上位の背骨です。  
USB / local IP / Ethernet / VPN などを含みます。

---

## 2. よく使う用語

### fabric
この repo 全体が作る「通信の土台」です。  
LoRa と Wi-Fi をまとめて 1 つの system として見せます。

### bearer
実際にデータを運ぶ運搬手段です。  
例:
- `lora`
- `wifi_mesh`
- `wifi_ip`
- `wifi_lr`
- `usb_cdc`

アプリには bearer 名をなるべく見せません。

### logical message kind
アプリが送るメッセージの意味です。  
例:
- `state`
- `event`
- `command`
- `command_result`
- `heartbeat`
- `manifest`
- `lease`
- `file_chunk`
- `fabric_summary`

### role
ノードのネットワーク上の役割です。  
例:
- `sleepy_leaf`
- `powered_leaf`
- `mesh_router`
- `mesh_root`
- `lora_relay`
- `dual_bearer_bridge`
- `gateway_head`
- `site_router`
- `controller_client`

### power class
電源条件です。  
例:
- `usb_powered`
- `mains_powered`
- `rechargeable_battery`
- `primary_battery`
- `energy_harvested`

### wake class
起き方です。  
例:
- `always_on`
- `sleepy_periodic`
- `sleepy_event`
- `maintenance_awake`

### mesh domain
Wi-Fi mesh 1 つ分の単位です。  
ESP-WIFI-MESH では mesh ID を共有する 1 ネットワークを意味します。 `[S21]`

### root
Wi-Fi mesh domain の最上位ノードです。  
router / host / upstream IP に接続する入口になります。 `[S20][S21]`

### relay
LoRa overlay で packet を中継するノードです。  
この repo では **powered / always-on node だけ** が relay になれます。

### bridge
2 つ以上の bearer をまたいで運べるノードです。  
例:
- Wi-Fi mesh ↔ LoRa
- Wi-Fi mesh ↔ IP
- LoRa ↔ USB host

### sleepy leaf
deep sleep を主体とする leaf node です。  
battery event node / battery telemetry node が代表です。  
**relay にはなれません。**

### summary codec
Wi-Fi の full payload を、LoRa で流せる小さな形に落とす codec です。  
例:
- full alarm detail → compact alarm summary
- full state blob → 1サンプル summary

### over-cap
今選ばれた bearer の payload cap を超える状態です。  
LoRa で over-cap になったら:
1. summary に落とす
2. Wi-Fi に逃がす
3. drop ではなく queue に残す
のどれかを選びます。

### hop
中継回数です。  
direct は 1 transmission、1 relay は 2 transmissions、2 relay は 3 transmissions を意味します。

### route class
message の性質に応じて fabric が選ぶ経路クラスです。  
例:
- `immediate_local`
- `normal_state`
- `critical_alert`
- `sparse_summary`
- `bulk_only`

### logical writer
event ledger / latest state / command ledger を最終的に確定する主体です。  
この repo では **Site Router** がそれです。

### Site Router
fabric の durable core です。  
dedupe、queue、lease、routing、summary、subscription を担当します。

### Host Agent
USB でつながった gateway head や mesh root を server 側から扱う daemon です。

### controller client
operator UI、automation service、別の Ubuntu server 上の worker など、fabric を利用する側です。  
command を出せますが、**logical writer ではありません**。

---

## 3. この repo の重要な割り切り

### 3.1 LoRaWAN clone は作らない
LoRaWAN は star-of-stars です。end device は single-hop で gateway と話します。 `[S23][S24][S25]`  
この repo は **raw LoRa 上の custom sparse mesh** を定義します。

### 3.2 Wi-Fi と LoRa は同じ役割にしない
- Wi-Fi mesh: 主骨格
- LoRa mesh: 長距離 overlay
- battery node: leaf
- powered node: router / relay になりうる

### 3.3 “自動最適化” は “何でも自由に切り替える” ことではない
この repo でいう自動最適化は、
- role
- power class
- payload size
- latency class
- current link health
- current duty headroom
を見て、**bounded policy** の中で route を選ぶことです。

### 3.4 低遅延制御は local に寄せる
サーボ PWM の閉ループや safety interlock は local controller で完結させるべきです。  
fabric は setpoint / mode / status を運びますが、**高速制御ループそのもの**には向きません。

---

## 4. どこから読むべきか

### ハードを理解したい
- `docs/01`
- `docs/02`
- `docs/03`

### まず設計全体を掴みたい
- `docs/05`
- `docs/06`
- `docs/11`

### payload / battery / mesh の痛いところを知りたい
- `docs/15`
- `docs/16`
- `docs/17`
- `docs/18`
- `docs/28`
- `docs/29`
- `docs/34`

### 複数サーバ・複数 root・複数 gateway をやりたい
- `docs/20`
- `docs/27`
- `docs/31`
- `docs/32`

### Codex で着手したい
- `docs/13`
- `docs/35`
- `backlog/epics-and-first-slices.md`
