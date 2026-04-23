# Goals, Scope, And Non-Goals

## 1. Product Goal

この repo の本当の目標は、特定プロジェクト専用の通信処理ではなく、

> **ESP32-S3 + SX1262 を使って、どんな IoT でも使える mesh-first hybrid communication fabric を作ること**

です。

そのために、この repo は次を狙います。

### GOAL-A
アプリ開発者が bearer を意識せず、
- 状態を送る
- イベントを送る
- 命令を送る / 受ける
- 心拍を監視する
- 大きいデータを送る
だけでよいこと。

### GOAL-B
network が site ごとに
- 小さく始められる
- 大きく育てられる
- battery node と powered node を混在させられる
- 1 server でも 2 server でも動く
- 1 gateway でも複数 root / relay でも動く
こと。

### GOAL-C
Wi-Fi と LoRa を同一 abstraction に収めつつも、  
**それぞれの得意領域を壊さない**こと。

### GOAL-D
自立 mesh を前提にして、
- Wi-Fi 側は self-organized / self-healing backbone
- LoRa 側は sparse / constrained relay overlay
- 上位では site router による durable routing
を成立させること。

---

## 2. Scope

## 2.1 In Scope

### A. reference hardware
- XIAO ESP32S3 + Wio-SX1262 kit
- 将来の custom board への移植を妨げない abstraction

### B. network fabric
- node SDK
- gateway head / mesh root firmware
- host agent
- site router
- client SDK / API

### C. message semantics
- `state`
- `event`
- `command`
- `command_result`
- `heartbeat`
- `manifest`
- `lease`
- `file_chunk`
- `fabric_summary`

### D. hybrid mesh
- Wi-Fi mesh backbone
- LoRa sparse mesh overlay
- multi-root / multi-gateway / multi-host ingress
- multiple controller client

### E. power-aware behavior
- battery sleepy leaf
- powered always-on router
- relay restrictions
- maintenance mode

### F. production reality
- JP region / output / antenna policy
- payload cap
- airtime awareness
- storage / queue / flash wear

---

## 3. Non-Goals

### NG-001
**LoRaWAN gateway の完全代替**は目標にしない。  
LoRaWAN 自体は star-of-stars であり、本 repo は raw LoRa custom mesh を対象にする。 `[S23][S24][S25]`

### NG-002
**すべてのノードが平等に relay する mesh**は目標にしない。  
battery node まで relay させる設計は採用しない。

### NG-003
**bulk / OTA / file transfer を LoRa mesh に流すこと**は目標にしない。

### NG-004
**サーボ制御や安全制御の閉ループをネットワーク越しにやること**は目標にしない。  
network は supervisory / setpoint / state reporting に使う。

### NG-005
**packet 単位で自由に bearer を踊らせること**は目標にしない。  
route class と hysteresis を持つ bounded policy を採用する。

### NG-006
**multi-master durable writer** を v1 標準にしない。  
logical writer は 1 つに保つ。

### NG-007
**ノードが PC / Ubuntu server の物理配置を意識すること**は目標にしない。  
node は `service` / `group` / `node` を主語にする。

---

## 4. Design Principles

### 4.1 powered backbone, sleepy leaves
always-on の powered node が骨格を作る。  
sleepy node は sensor / event leaf に徹する。

### 4.2 route by meaning, not by bearer
アプリは「どの bearer を使うか」ではなく「何を送りたいか」を宣言する。

### 4.3 local-first
site 内で閉じていても ingest / state / command / recovery が継続する。

### 4.4 durable core at the site
radio head / mesh root は durable writer にならず、Site Router が ledger を持つ。

### 4.5 scale by domains
site を 1 枚の flat ネットワークで考えず、
- Wi-Fi mesh domain
- LoRa relay zone
- Fabric Spine
の重ね合わせで考える。

### 4.6 summary over impossible payload
LoRa に入らないものは「無理に fragmentation」より、
- summary codec
- defer to Wi-Fi
- local retention
を優先する。

---

## 5. What “mesh” means in this repo

この repo で mesh は 3 段階あります。

### 5.1 radio mesh
Wi-Fi mesh / LoRa relay overlay のこと。

### 5.2 path mesh
1 つの message が複数の possible path を持てること。

### 5.3 control mesh
複数 root / gateway / host / server が 1 fabric の一部として協調できること。

ユーザーが欲しい “めちゃスケールも縮小もできる” という感覚は、  
この 3 段を重ねることで実現します。
