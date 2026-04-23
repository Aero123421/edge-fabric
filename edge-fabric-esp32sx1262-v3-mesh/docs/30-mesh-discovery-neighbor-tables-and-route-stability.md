# Mesh Discovery, Neighbor Tables, And Route Stability

## 1. Discovery を雑にすると壊れる

mesh で難しいのは、単に packet を送ることではなく、
- 誰が近いか
- 誰が元気か
- 誰が詰まっているか
- どこへ行けば site の core に届くか
を適度なコストで知ることです。

---

## 2. Wi-Fi mesh discovery

Wi-Fi mesh の parent/child discovery 自体は ESP-WIFI-MESH が担います。 `[S20][S21]`  
repo はその上に fabric metadata を乗せる。

必要な観測:
- current parent
- parent changed count
- root present
- layer/depth
- child count
- root upstream reachable
- queue toDS backlog

---

## 3. LoRa discovery

LoRa 側は custom overlay なので、自前で軽い discovery が必要。

### source of truth
- relay beacons
- successful ack history
- maintenance survey
- last-good path memory

### not allowed
- constant flood
- sleepy node continuous listen
- verbose route exchange

---

## 4. Neighbor table design

neighbor table は role ごとに違う。

### sleepy_leaf
small table only:
- 2〜4 known parents
- last success age
- profile bits

### lora_relay
richer table:
- multiple neighbors
- route metrics
- queue pressure snapshots
- advertised depth

### mesh_root / bridge
both Wi-Fi and LoRa side observations

---

## 5. Route stability tools

### 5.1 hold-down timer
失敗直後にすぐ戻らない。

### 5.2 success hysteresis
少し良い程度では切り替えない。

### 5.3 stale aging
古い neighbor 情報は弱める。

### 5.4 failure buckets
連続失敗数を覚える。

### 5.5 lease hints
Site Router が preferred parent / gateway set を配る。

---

## 6. Discovery modes by wake class

### always_on
- active discovery allowed
- richer neighbor table
- periodic beacons allowed

### sleepy_periodic
- bounded scan
- passive memory reuse
- no aggressive discovery

### sleepy_event
- event-first
- discovery only when necessary or maintenance

### maintenance_awake
- full survey allowed

---

## 7. Domain and zone hints

site policy は node に以下の hint を返せる。
- preferred mesh domain
- preferred root set
- preferred LoRa relays
- maximum allowed hop
- path class restrictions
- quiet hours / maintenance windows

これにより “完全フリーの mesh” ではなく、  
**自立しつつ site policy で誘導された mesh** を作る。

---

## 8. Route failure sequence

### Wi-Fi parent lost
1. current parent retry
2. parent reselection
3. domain degraded if repeated
4. fallback path if class allows

### LoRa relay lost
1. retry same relay (bounded)
2. alternate relay
3. direct if possible
4. queue/defer
5. degraded-lora

---

## 9. Why this matters for battery

sleepy leaf が discovery に時間を使うと、
- wake time 増
- RX window 増
- battery 消費増
- network noise 増
になります。

したがって discovery は role-aware でなければならない。
