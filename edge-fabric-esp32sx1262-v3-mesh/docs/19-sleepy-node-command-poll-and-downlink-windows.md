# Sleepy Node Command Poll And Downlink Windows

## 1. この文書の前提

sleepy node は “いつでも受けられる node” ではありません。  
したがって command delivery を **poll / bounded receive window** 前提で定義します。

---

## 2. Command service levels

### immediate
always-on powered node のみ。

### bounded
powered node / router / root / bridge。  
ms〜秒オーダーで期待。

### eventual_next_poll
sleepy leaf。  
次回 wake / uplink / poll 時に受ける。

### unsupported
大きい / interactive / ultra-low-latency command。

---

## 3. Basic sleepy downlink model

1. node wakes
2. uplink event/state/heartbeat
3. keeps RX window for short time
4. parent/root/gateway may deliver:
   - ack
   - pending command digest
   - tiny command stub
5. node either:
   - apply immediately if tiny and safe
   - store for next cycle
   - reject unsupported
6. sleep

---

## 4. Poll styles

### 4.1 implicit poll
uplink 自体を poll と見なす。  
最も battery friendly。

### 4.2 explicit tiny poll
“pending command ある？” を tiny packet で聞く。  
特殊ケースのみ。

### 4.3 maintenance poll
maintenance_awake 中に longer session で command/config sync する。

---

## 5. Downlink classes for sleepy nodes

### tiny-control
- threshold
- mode bit
- latch clear
- next interval
- enable/disable flags

### tiny-config
- lease generation update
- parent hint refresh
- route policy byte
- summary codec version

### not for sleepy LoRa downlink
- file
- full config blob
- long reason strings
- complex multi-step transactions

---

## 6. Command safety rules

### RULE-1
sleepy node に送る command は idempotent を基本にする。

### RULE-2
effect が大きい command は `expires_at` を持つ。

### RULE-3
sleepy node は stale command を実行しない。

### RULE-4
immediate actuation を sleepy node に期待しない。

### RULE-5
command_result は tiny summary を返せること。

---

## 7. Pending command digest

sleepy node に full command list を返すのではなく、まず digest を返してよい。

例:
- pending count
- newest command id short
- expires soon bit
- urgent flag

node は digest を見て、
- tiny command を受ける
- maintenance を要求する
- next poll で続ける
を選べる。

---

## 8. Event-driven wake special case

PIR や leak node は event wake 直後に uplink する。  
このタイミングで tiny command を返せると便利。

例:
- “quiet 10 min”
- “re-arm”
- “change threshold profile”
- “send one more sample”

---

## 9. Maintenance_awake path

USB 接続 / service trigger / local button などで maintenance_awake に入ったら、
- Wi-Fi attach
- lease refresh
- full config sync
- diagnostic upload
- OTA
を許可する。

つまり、普段の sleepy contract と、保守時の rich contract を分ける。

---

## 10. What this means for developers

開発者は sleepy node を使う時、
“この node は eventually reachable” と理解する必要がある。  
fabric はそれを隠しすぎない。

したがって SDK では command API に少なくとも:
- `service_level`
- `expected_delivery`
- `expires_at`
- `idempotency_key`
が必要。
