# Payload Cookbook And Message Packing Examples

## 1. この文書の目的

“実際に何バイトぐらいで何を送るのか” を具体例で固定します。  
特に LoRa relay path では tiny summary を意識する必要があります。

---

## 2. LoRa direct / relay を意識した目安

### JP125_LONG_SF10
- direct app body: ~10 B
- relayed app body: ~6 B

### JP125_BAL_SF9
- direct app body: ~34 B
- relayed app body: ~30 B

### JP250_FAST_SF8
- direct app body: ~66 B
- relayed app body: ~62 B

---

## 3. Tiny alarm summary (6–10 B)

### use case
- PIR triggered
- leak detected
- door opened
- vibration alarm

### suggested fields
- 1B kind/alarm code
- 1B severity
- 2B source short or sample value
- 1B battery bucket
- 1B flags
- optional 2B coarse time delta
- optional 1B route/debug nibble

### when to use
- direct LoRa
- 1-relay LoRa
- critical redundant path

---

## 4. Sparse telemetry summary (8–20 B)

### use case
- temperature
- humidity
- tank level
- pulse count delta

### suggested fields
- 1B measurement kind
- 2B value
- 1B unit/profile scale
- 1B battery bucket
- 1B quality flags
- 2B sample age / coarse tick
- optional 2B delta2 or min/max packed

### notes
複数サンプル履歴を積み始めるとすぐ膨らむ。  
LoRa summary では “最新値 + 状態ビット” を基本にする。

---

## 5. Tiny command stub (4–12 B)

### use case
sleepy node への next-poll command:
- threshold update
- quiet period
- alarm latch clear
- sampling interval
- mode bit

### suggested fields
- 1B command code
- 1B arg0
- 1B arg1
- 1B flags
- optional 2B expires short
- optional 2B command short id

---

## 6. Command result compact (4–16 B)

### use case
- success/fail
- applied profile id
- current mode
- short reason code

### suggested fields
- 1B result code
- 1B reason code
- 1B current mode
- 1B flags
- optional 2B applied value
- optional 2B queue age
- optional 2B command short id

---

## 7. Heartbeat summary (6–14 B)

### use case
sleepy node piggyback or relay health.

### suggested fields
- 1B battery bucket
- 1B reset/wake bits
- 1B queue depth bucket
- 1B fault bits
- 2B coarse uptime/wake counter
- optional 2B firmware short
- optional 2B lease generation short

---

## 8. Powered node compact state (16–40 B)

### use case
Wi-Fi mesh regular state.

### suggested fields
- mode
- setpoint
- current value
- fault bits
- queue depth
- local temperature
- link health short
- actuator summary
- maybe one extra sensor pair

これは Wi-Fi mesh なら普通に扱える。  
LoRa へ落とすなら別 summary codec が必要。

---

## 9. Servo controller payload split

### Wi-Fi command full shape
- command id
- target angle
- speed profile
- acceleration profile
- timeout
- safety mode
- request origin

### LoRa fallback summary
- command stub code
- angle short
- safety stop / preset profile only

### design rule
LoRa で任意角度の rich control session をしない。  
preset / safe-mode / stop 程度に絞る。

---

## 10. PIR event pattern

### local
- immediate local buzzer/light
- local debounce

### Wi-Fi path
- full event to controller/UI

### LoRa path
- `pir_triggered summary`
  - kind
  - severity
  - battery
  - coarse tick
  - maybe area code

---

## 11. Leak detection pattern

### Wi-Fi full
- sensor id
- current raw value
- filtered value
- wet duration
- temperature
- local diagnostics

### LoRa summary
- leak code
- severity
- wet duration bucket
- battery bucket
- source short id

---

## 12. What not to pack into LoRa summary

- long strings
- JSON keys
- full UUIDs
- absolute ISO timestamps
- verbose diagnostics
- multiple device identities
- large maps / arrays

---

## 13. Packing principles

### P1
enumeration and bitfields first

### P2
use scaled integers, not floats

### P3
use short IDs allocated by lease

### P4
send latest and most important, not whole history

### P5
if humans need detail, send trigger now and fetch detail later over Wi-Fi

---

## 14. Golden rule

LoRa payload を設計する時は、
**“何を省くか”** が主題です。  
Wi-Fi payload を設計する時は、
**“何を持たせるか”** が主題です。
