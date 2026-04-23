# Multi-Gateway / Multi-Root ACK Owner And Downlink Arbitration

## 1. 問題設定

site に
- USB gateway が複数
- Wi-Fi mesh root が複数
- LoRa relay bridge が複数
あると、同じ message が複数経路で見えるし、下りも複数候補になります。

この文書は、その時に
- 誰が ACK を返すのか
- どこから downlink するのか
を固定します。

---

## 2. ACK levels

### 2.1 Hop ACK
次ホップが queue に受けたこと。  
LoRa relay / Wi-Fi mesh next-hop 単位。

### 2.2 Path ACK
selected path owner が path 上で受理したこと。  
例:
- chosen gateway
- chosen mesh root
- chosen bridge

### 2.3 Persist ACK
Site Router が durable ledger に保存したこと。

### 2.4 Command phase ACK
command life-cycle の更新。

---

## 3. ACK owner rules

### RULE-1
Hop ACK の owner は “直前の next-hop” である。

### RULE-2
Persist ACK の owner は “Site Router” である。

### RULE-3
node app が最終的に安心すべき ACK は Persist ACK または Command phase ACK である。

### RULE-4
gateway / root は durable completion を勝手に代表しない。

---

## 4. Multi-ingress duplicate

同じ `event_id` / `message_id` が
- Wi-Fi path
- LoRa path
- gateway A
- gateway B
から同時に入ってよい。

Site Router は
- dedupe
- ingress metadata retain
- best-observation select
を行う。

best-observation の例:
- earliest receive
- highest confidence
- best RSSI/SNR
- lowest hop count

---

## 5. Downlink arbitration

下り候補が複数ある時、次の順で選ぶ。

### 5.1 If target is powered Wi-Fi node
- current mesh domain route
- last-good mesh root
- current parent known path

### 5.2 If target is sleepy LoRa leaf
- last-good gateway/relay chain
- shortest hop path under current policy
- known awake window compatibility

### 5.3 If target supports redundant path
- immediate primary
- fallback secondary
- duplicate only if policy says so

---

## 6. Arbitration inputs

- target role / wake class
- current domain reachability
- last_good_path
- recent hop failures
- relay queue pressure
- duty headroom
- command urgency
- payload size after summary
- whether target is currently awake

---

## 7. Domain-aware selection

mesh_root A と mesh_root B がある時、  
target が `mesh_domain_id = D2` なら、まず D2 側の root を使う。

もし D2 が degraded なら:
- bridge 経由
- alternate domain
- LoRa summary fallback
の順に試す。

---

## 8. Sleepy target special case

sleepy node は “今起きているか” が重要。

したがって downlink scheduler は、
- just-seen uplink
- expected RX window
- periodic schedule hint
- maintenance_awake flag
を使って dispatch する。

“起きてない battery node に immediate control を何度も打つ” はしない。

---

## 9. Gateway / root failure cases

### gateway head failure
- Host Agent detects disconnect
- pending path marked degraded
- alternate gateway chosen if exists

### mesh root failure
- domain enters degraded
- parent reselection / root switch may occur on Wi-Fi mesh `[S20][S21]`
- Site Router updates preferred ingress set

### relay failure
- route invalidated
- fallback direct or alternate relay attempted
- if none, queue/defer and mark degraded-lora

---

## 10. Why this matters

複数 server / 複数 root / 複数 gateway を許可しても、  
ACK と downlink owner を曖昧にすると command が二重実行されます。

だから、
- route は複数でよい
- ingress は複数でよい
- でも final settle は 1 つ
という原則が必要。
