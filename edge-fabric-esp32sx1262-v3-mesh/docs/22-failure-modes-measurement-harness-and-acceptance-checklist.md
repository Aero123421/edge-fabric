# Failure Modes, Measurement Harness, And Acceptance Checklist

## 1. この文書の目的

仕様が細かくなっても、  
**測れないものは実装したことにならない** ので、  
field で壊れやすい点を最初から checklist 化します。

---

## 2. 重要 failure mode 一覧

## 2.1 battery leaf が relay をしようとして電池が持たない
症状:
- sleep 電流は低いのに実 battery life が短い
- listen time が想定外に長い
- wake-on-radio 実装が複雑化

対策:
- battery leaf relay 禁止
- relay / bridge は powered only
- role lease で明示許可以外は false

## 2.2 LoRa payload over
症状:
- encode 後の payload が profile cap を超える
- fragmentation に逃げたくなる

対策:
- summary codec 必須
- Wi-Fi only shape を用意
- bulk LoRa fallback 禁止
- encoder unit test で cap check

## 2.3 sleepy node へ low-latency command を期待
症状:
- “command が届かない”
- 実際は node が寝ているだけ

対策:
- command service level を API で明示
- `next_poll` と `unsupported` を分ける
- UI に expectancy を出す

## 2.4 multi-gateway ACK collision
症状:
- uplink は見えるのに ACK が不安定
- 複数 gateway が同時送信

対策:
- `ack_owner_gateway_id`
- non-owner は listen-only ingress

## 2.5 Wi-Fi connect storm on battery node
症状:
- wake のたびに Wi-Fi scan / assoc
- battery drain
- sleep budget 崩壊

対策:
- deployed battery leaf で Wi-Fi disabled default
- connect budget mandatory
- maintenance_awake を分離

## 2.6 flash wear / write amplification
症状:
- normal telemetry で storage activity 多発
- compaction が多い
- wake time が伸びる

対策:
- critical only durable
- normal coalesce
- heartbeat piggyback
- storage write counters を metrics 化

## 2.7 JP region misconfiguration
症状:
- global frequency / power profile が使われる
- certification assumptions 崩壊

対策:
- RegionPolicy::JP mandatory in prod
- lab build と prod build を分ける
- boot log に region digest を出す

## 2.8 CAD-only LBT
症状:
- “CAD してるから carrier sense してるはず” という誤解

対策:
- JP build は CAD-only 不可
- energy detect / RSSI based LBT path を別実装

## 2.9 gateway head の sleep で USB 切断
症状:
- host から device が消える
- operator が “壊れた” と誤認

対策:
- gateway head always_on
- auto sleep 無効
- watchdog/reboot policy は別に持つ

---

## 3. measurement harness で最低限測るもの

## 3.1 LoRa path
- payload size vs encode result bytes
- chosen profile
- airtime estimate
- uplink success rate
- Link ACK rate
- Persist ACK rate
- RSSI / SNR per gateway
- duplicate ingest count

## 3.2 Wi-Fi path
- connect attempt count
- connect time
- connect success rate
- payload send latency
- retry count

## 3.3 power path
- wake duration per cycle
- time in sensor warm-up
- time in encode
- time in TX
- time in RX1/RX2
- time in Wi-Fi connect attempt
- storage write count
- deep sleep current
- average current over scripted workload

## 3.4 queue / storage
- critical queue depth
- normal coalesced key count
- dropped normal count
- flash write count
- compaction count

---

## 4. acceptance criteria のたたき台

## 4.1 battery sparse node
- deployed mode で relay disabled
- Wi-Fi connect attempt = 0 by default
- critical event summary が `JP125_LONG_SF10` に収まる
- normal telemetry は coalesce される
- 1000 wake cycles で queue corruption なし

## 4.2 powered leaf
- Wi-Fi primary path で command latency stable
- LoRa fallback summary あり
- large payload が LoRa に流れない
- maintenance / diagnostics path が機能

## 4.3 multi-gateway site
- same uplink の dedupe 成功
- owner gateway 以外が immediate ACK を返さない
- owner failover 後に downlink 成功

## 4.4 gateway head
- sleep に入らず USB が安定
- host reconnect 後の session recovery 成功
- no durable writer responsibility

---

## 5. lab で必ずやるべき試験

1. payload boundary test  
2. over-cap fallback test  
3. no-summary reject test  
4. sleepy command expiry test  
5. multi-gateway ACK collision test  
6. flash write budget soak test  
7. maintenance_awake transition test  
8. region policy boot block test  
9. deep sleep 連続 cycle test  
10. queue overflow anomaly test

---

## 6. field pilot 前の gate

### Gate A
LoRa compact payload boundary が unit test で固定されていること

### Gate B
battery deployed profile で Wi-Fi storm が起きないこと

### Gate C
JP region profile が build-time / runtime で固定されていること

### Gate D
multi-gateway owner arbitration が再現試験で通ること

### Gate E
critical event が queue full でも silent drop しないこと

---

## 7. この文書の結論

deep-dive で増えた仕様は、  
**failure mode を潰すために必要な具体化**です。

Codex 実装では、この checklist を test plan と一緒に repo に残し、  
“動いた” ではなく **“壊れ方が制御されている”** を確認すべきです。
