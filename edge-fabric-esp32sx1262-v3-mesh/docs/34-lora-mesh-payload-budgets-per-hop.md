# LoRa Mesh Payload Budgets Per Hop

## 1. Why per-hop budget matters

LoRa packet の radio payload cap は hop が増えても変わりません。  
しかし site 全体で見ると、relay hop が増えるほど **effective channel occupancy** は増えます。

さらに、relay header を入れると user body も減ります。

---

## 2. Direct vs relayed body cap

repo の v1 design target:
- direct uplink overhead: 14 B
- relayed uplink overhead: 18 B

これを現在の profile cap に当てると、app body は次のようになります。

| Profile | Total cap | Direct app body | Relayed app body |
|---|---:|---:|---:|
| JP125_LONG_SF10 | 24 B | 10 B | 6 B |
| JP125_BAL_SF9 | 48 B | 34 B | 30 B |
| JP250_FAST_SF8 | 80 B | 66 B | 62 B |
| JP250_CTRL_SF9 | 64 B | 50 B | 46 B |

### meaning
`JP125_LONG_SF10` で relay を通すなら、  
**本当に tiny summary しか載らない**。

---

## 3. Effective site occupancy

same packet が
- direct: 1 transmission
- 1-relay: 2 transmissions
- 2-relay: 3 transmissions
になる。

つまり effective occupancy は概算で:
- direct = TOA × 1
- 1-relay = TOA × 2
- 2-relay = TOA × 3

---

## 4. Example intuition

`JP125_LONG_SF10` は raw TOA が重い profile です。  
critical alert summary には使えるが、
- relay を増やす
- 頻度を増やす
- payload を増やす
を同時にやるとすぐ苦しくなる。

だから:
- critical alert は OK
- regular chatty telemetry は NG
- relay chain は短く
が必要。

---

## 5. Design consequences

### consequence A
far battery node はなるべく 1 relay までで拾う。

### consequence B
2 relay は “必要な critical edge” に絞る。

### consequence C
もし summary でも足りないなら topology を変える。  
payload を増やして LoRa に押し込まない。

### consequence D
遠い場所に powered bridge を置いて、そこから Wi-Fi local island を作る方が良い場合が多い。

---

## 6. What to pack in 6–10 bytes

`JP125_LONG_SF10` で direct / relay を考えると、summary body は 6–10 B が現実的です。

例:
- 1B alarm code
- 1B severity
- 2B value
- 1B battery bucket
- 1B flags
- 2B coarse tick / delta
- 1B checksum-ish extra flag

これでかなりの異常通知は作れる。

---

## 7. What not to pack

- long IDs
- long timestamps
- strings
- repeated key names
- multiple sample histories
- human-readable payloads

---

## 8. Design rule of thumb

LoRa mesh summary は、
**「人が読むメッセージ」ではなく「機械が後で full detail を引きに行くための trigger」**
だと考えると設計しやすい。
