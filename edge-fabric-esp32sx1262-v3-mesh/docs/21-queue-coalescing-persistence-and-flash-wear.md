# Queue Coalescing, Persistence, And Flash Wear Policy

## 1. この文書の目的

queue / retry は必要です。  
しかし node 側、特に sleepy battery node では、**何でも durable にすると flash を痛める**し、  
逆に全部 volatile にすると critical event を失います。

この文書では、node 側 queue の保存方針を固定します。

---

## 2. 3種類に分ける

node queue は少なくとも 3 種類に分ける。

## 2.1 critical durable outbox
失うと困るもの。
- critical event
- command result short
- provisioning / identity transition ack
- explicit operator-visible anomaly

## 2.2 normal coalescing state buffer
失っても “最新だけ残ればよい” もの。
- periodic telemetry
- normal state
- health sample
- repeated summary

## 2.3 forbidden-on-node bulk
node 上に長く積まないもの。
- file chunk
- OTA payload
- verbose log stream
- image / waveform

---

## 3. sleepy battery node の標準方針

## 3.1 MUST
`critical` だけ durable。

## 3.2 SHOULD
`normal state` は latest only で coalesce。

## 3.3 MUST NOT
heartbeat のたびに flash write しない。

## 3.4 MUST NOT
sequence / metric を sample ごとに永続化しない。

---

## 4. なぜ per-sample durable write を禁止するか

machineQ の guide でも、battery life estimation では
- sleep
- idle
- running
- LoRa TX
- LoRa RX

を分けて見積もるべきだと説明されています。 `[S19]`

この repo ではそこに加えて、**storage write cost と wear** も見るべきだと考えます。

flash write を頻発させると:

- active current が伸びる
- wake budget が伸びる
- erase / compaction が発生する
- lifetime が読みにくくなる

よって、保存対象を絞る。

---

## 5. node 側で durable にすべきもの

v1 default で durable 推奨なのは以下。

- critical alert summary
- command result short
- command acceptance evidence (必要時)
- boot / reprovision transition marker
- operator-visible fault

### 具体例
- 漏水検知 summary
- contact open/close critical edge
- “command slot 3 succeeded” short result
- “radio profile invalid, fell back to safe mode”

---

## 6. coalesce してよいもの

- 温度周期送信
- バッテリー残量
- RSSI 統計
- 直近状態の summary
- same sensor の repeated normal value

### ルール
同じ `coalesce_key` を持つ normal state は、
- newest だけ残す
- count / min / max / avg を短く持つ
- full 履歴は node に残さない

---

## 7. heartbeat は packet でも storage でも “別扱いしすぎない”

heartbeat は重要だが、
- 毎回独立 packet
- 毎回独立 flash record

にするとコストが高い。

方針:
- state/event uplink に piggyback 優先
- `last_seen`, `battery`, `fault_bits`, `queue_depth` をまとめて送る
- 単独 heartbeat は long silence 時だけ

---

## 8. RTC memory / RAM / Flash の使い分け

## 8.1 RAM
- current working queue
- temporary encode buffer
- current cycle metrics

## 8.2 RTC memory
- deep sleep across reboot-like wake で残したい少量の volatile state
- wake counter
- last short sequence hint
- pending summary aggregate

### 注意
RTC memory は durable の代わりではない。

## 8.3 Flash / NVS / littlefs / ring
- critical durable outbox
- provisioning state
- lease generation marker
- last known safe config

---

## 9. queue budget の標準

### 9.1 sleepy battery node
- critical durable slots: 16
- normal coalescing entries: 8 keys
- pending command result slots: 4
- bulk: 0

### 9.2 powered leaf
- critical durable slots: 64
- normal queue/coalesce: 32〜128
- Wi-Fi full payload retry queue: allowed
- bulk: bounded, but local storage policy 依存

### 9.3 gateway head
- durable canonical queue: 持たない
- short-term spool only

---

## 10. queue overflow 時のルール

## 10.1 critical queue full
- silent drop 禁止
- operator-visible anomaly を作る
- least-recent non-ack critical を簡単に捨てない
-必要なら summary-of-overflow を作る

## 10.2 normal queue full
- oldest normal を捨てる / coalesce
- latest state を優先
- verbose metrics を切る

## 10.3 command result queue full
- result short を優先
- normal telemetry より優先度を上げる

---

## 11. retry と storage の境界

radio retry と durable outbox retry を分ける。

### 11.1 radio retry
同一 wake cycle 内の短い retry。  
storage 追加 write を毎回行わない。

### 11.2 durable outbox retry
次回 wake cycle 以降に再送。  
critical only を原則にする。

---

## 12. deep sleep node の write timing

node は次のタイミングでだけ durable write してよい。

- new critical event 発生時
- command result short 確定時
- config / lease generation change 時
- fault / anomaly transition 時

### やってはいけない
- 送信試行ごと
- ACK 待ちごと
- sample ごと
- RSSI 更新ごと

---

## 13. flash wear を減らすコツ

- append-only ring を使う
- compaction はまとめてやる
- heartbeat を piggyback する
- normal telemetry は aggregate / coalesce
- sequence は RTC / RAM 優先
- schema_version や long strings を record 毎に重複保存しない

---

## 14. pending command の保存

sleepy node 側では full command queue を持たない。  
基本は “受け取った 1 slot を今 cycle で処理する” だけ。

もし次 cycle まで持ち越す必要があるなら、
- slot id
- desired_state_version
- minimal compact body

だけ durable に持つ。

---

## 15. Site Router 側との役割分担

Node:
- minimum durable evidence
- immediate retry / next wake retry
- latest state coalesce

Site Router:
- canonical event ledger
- full dedupe
- dead-letter
- audit retention
- command lifecycle source-of-truth

つまり durable の本丸は router。  
node durable はあくまで最小限。

---

## 16. acceptance test

- normal telemetry 連打で flash write count が暴れない
- critical event 発生時のみ durable append
- queue full で normal は coalesce
- critical full で anomaly 生成
- deep sleep 何百 cycle 後も boot/retry が破綻しない
- power cut 直後でも critical outbox が回復する

---

## 17. この文書の結論

- **node durable queue は “全部保存” ではなく “失えないものだけ保存”**
- **normal state は coalesce する**
- **heartbeat は piggyback を基本にする**
- **flash write は event-driven に限定する**
- **router を durable 正本にし、node は最小 durable に留める**
