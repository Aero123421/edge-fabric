# Power Classes, Sleepy Nodes, And Role Behavior

## 1. この文書の結論

- **battery / deep sleep node は leaf 専用**
- **mesh の骨格は always-on powered node が担う**
- **sleepy node に low-latency push command を期待しない**
- **sleepy node は Wi-Fi 常時接続ノードとして扱わない**
- **sleepy node の fabric 契約は uplink-first / bounded downlink**

この文書は、その理由と細かい運用条件を固定します。

---

## 2. Power classes

### 2.1 usb_powered
- PC / server / AC adapter で給電
- always_on 前提を置きやすい

### 2.2 mains_powered
- AC 常時給電
- router / root / relay に向く

### 2.3 rechargeable_battery
- 充電式
- sleepy も maintenance_awake も取りやすい

### 2.4 primary_battery
- 交換式
- aggressive relay / Wi-Fi attach は避けたい

### 2.5 energy_harvested
- ソーラ・振動など
- sleep 主体
- relay/backbone 禁止

---

## 3. Wake classes

### always_on
常時起動。  
mesh_router / mesh_root / lora_relay / powered control node 向け。

### sleepy_periodic
タイマ起床。  
metering / periodic telemetry 向け。

### sleepy_event
外部割り込み起床。  
PIR / door / leak / vibration 向け。

### maintenance_awake
保守時だけ長く起きる。  
Wi-Fi, BLE, OTA, diagnostics をここに寄せる。

---

## 4. Why sleepy node cannot be a relay

### 4.1 radio reality
relay には、
- 親から受ける
- 子へ送る
- ACK を返す
- route を学習する
- neighbor を監視する
が必要です。

### 4.2 power reality
LoRa relay whitepaper でも、relay の電力の多くは retransmit ではなく **listening** に使われると説明されています。 `[S17]`

### 4.3 ESP32-S3 reality
deep sleep では Wi-Fi / Bluetooth connection は維持されません。 `[S15]`

### 4.4 design conclusion
したがって、
- `sleepy_leaf MUST NOT relay`
- `sleepy_leaf MUST NOT backbone`
- `sleepy_leaf MUST NOT be ack_owner`
- `sleepy_leaf MUST NOT accept parent duties`

---

## 5. Sleepy leaf lifecycle

## 5.1 periodic telemetry leaf
1. timer wake
2. minimal boot
3. sensor sample
4. local rule evaluate
5. compact message build
6. route choose (`LoRa direct` first)
7. uplink
8. bounded RX window
9. optional pending command fetch
10. sleep

## 5.2 event leaf
1. IRQ wake
2. cause read
3. immediate event summary build
4. uplink
5. short RX window
6. sleep

## 5.3 maintenance_awake
1. external power detect / service trigger
2. long awake mode
3. Wi-Fi/BLE enable
4. verbose diagnostics / OTA / re-lease
5. leave maintenance mode
6. resume deployed profile

---

## 6. Sleepy leaf route behavior

### 6.1 default
- primary: LoRa direct
- fallback: LoRa via known relay
- maintenance only: Wi-Fi

### 6.2 not allowed by default
- Wi-Fi attach every wake
- full network scan every wake
- general route discovery flood
- acting as route advertisement source every short interval

### 6.3 allowed in maintenance_awake
- Wi-Fi scan
- re-commission
- relay list refresh
- firmware update
- full diagnostics upload

---

## 7. Sleepy leaf command model

sleepy leaf は command target になってよい。  
ただし service level は `eventual / next-poll` です。

### support
- parameter change
- mode switch
- threshold update
- quiet period set
- alarm latch reset
- sampling interval update

### avoid
- sub-second control
- interactive streaming
- large config bundle
- closed-loop actuation
- non-idempotent “今すぐ一発だけやって” 系 command

---

## 8. Known-parent list

sleepy leaf は aggressive discovery を避けるため、NVS に small parent list を保持します。

保持する候補:
- `last_good_gateway_or_relay`
- `secondary_known_parent`
- `recent_success_profile`
- `last_success_rssi_or_snr_bucket`
- `lease_generation`
- `failure_backoff`

wake ごとに全部更新しない。  
成功時や maintenance_awake 時など、必要な時だけ更新する。

---

## 9. Heartbeat strategy for sleepy nodes

### 9.1 piggyback first
heartbeat 専用 packet を乱発しない。

### 9.2 include in normal uplink
- battery %
- reset cause
- wake cause
- queue depth
- relay failure count
- lease generation short
- firmware digest short

### 9.3 standalone heartbeat only when needed
- 長時間無通信
- operator が生死確認を強く要求
- fabric policy が許可
- airtime budget 内

---

## 10. Always-on powered roles

以下は always_on を前提にする。

- `mesh_router`
- `mesh_root`
- `lora_relay`
- `dual_bearer_bridge`
- `gateway_head`
- `site_router`
- immediate command target を担う `powered_leaf`

これらは:
- parent/child
- relay queue
- root uplink
- local control
を担当してよい。

---

## 11. Device classes and examples

| Example | Power | Wake | Recommended role |
|---|---|---|---|
| PIR battery sensor | primary_battery | sleepy_event | sleepy_leaf |
| water leak pad | coin / primary | sleepy_event | sleepy_leaf |
| environment logger | rechargeable | sleepy_periodic | sleepy_leaf |
| servo controller | mains | always_on | powered_leaf |
| building corridor bridge | mains | always_on | dual_bearer_bridge |
| roof relay box | solar+big battery? no by default | always_on only if engineered | lora_relay (special) |
| Ubuntu-connected radio | usb | always_on | gateway_head |

---

## 12. Non-obvious but important rules

### 12.1 “バッテリーが十分大きいから relay にしてよい” を標準にしない
特殊設計としてはあり得るが、repo の default にはしない。

### 12.2 sleepy leaf に mesh discovery flood をさせない
電池と airtime を無駄にする。

### 12.3 battery node の Wi-Fi maintenance path は “普段オフ”
保守時だけオンにする。

### 12.4 battery node は “たまに起きる賢い leaf”
骨格ではなく、edge sensing specialist と考える。
