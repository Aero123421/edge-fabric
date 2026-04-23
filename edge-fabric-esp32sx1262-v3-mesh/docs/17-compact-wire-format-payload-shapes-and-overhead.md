# Compact Wire Format, Payload Shapes, And Overhead

## 1. この文書の目的

payload 問題をちゃんと扱うためには、**論理 payload** と **on-wire payload** を分ける必要があります。  
特に LoRa mesh では、relay header が増えるぶん user payload がさらに削られます。

---

## 2. Three payload shapes

## 2.1 full shape
Wi-Fi / IP で使う通常形。  
CBOR / JSON / protobuf などを許可できる。

### 用途
- detailed telemetry
- command detail
- file chunk
- diagnostics
- OTA metadata

## 2.2 compact shape
Wi-Fi mesh / small control / efficient state 用。  
CBOR or compact binary。

### 用途
- regular telemetry
- command result
- mesh health
- root/router summary

## 2.3 summary shape
LoRa 用の要約形。  
**人間に読みやすい JSON ではなく、機械向け compact binary** を前提にする。

### 用途
- critical alert summary
- sparse state
- tiny command stub
- heartbeat summary

---

## 3. On-wire envelope levels

### 3.1 full envelope
host/client/fabric spine 向け。  
冗長な field を持ってよい。

### 3.2 compact envelope
Wi-Fi mesh / local bridge 向け。  
文字列は極力避ける。

### 3.3 ultra-compact LoRa envelope
LoRa summary 向け。  
short IDs, flags, compact counters を使う。

---

## 4. Direct vs relayed overhead

この repo では、v1 設計値として次を置きます。

### 4.1 direct LoRa minimum overhead
- uplink: **14 B**
- downlink control: **16 B**

### 4.2 relayed LoRa minimum overhead
relay header のため、
- uplink: **18 B**
- downlink control: **20 B**

relay header が増える理由:
- hop_limit
- hop_count
- last_hop / prev_hop short id
- route flags

---

## 5. Why strings are poison on LoRa

LoRa summary packet に
- 長い topic 名
- UUID 文字列
- JSON key 名
- 冗長な timestamp 文字列
を載せるとすぐ cap を超えます。

したがって LoRa summary では:
- topic 名ではなく short message type
- hardware_id ではなく fabric_short_id
- ISO8601 ではなく compact tick / delta
- boolean packs / bit fields
を使う。

---

## 6. Suggested compact fields

### 6.1 common
- `v` version nibble
- `k` kind code
- `p` priority code
- `sid` short source id
- `fid` flow / event / command short id
- `ssn` session short
- `seq` seq short
- `flags`

### 6.2 relay/meta
- `hc` hop count
- `hl` hop limit
- `lh` last hop
- `rc` route class

### 6.3 payload body
kind ごとの codec に渡す小さい body。

### 6.4 auth
short tag or truncated tag

---

## 7. Full shape vs summary shape example

### alarm full shape (Wi-Fi)
- sensor kind
- location string
- raw values
- thresholds
- last N samples
- diagnostic flags
- human note
- firmware info

### alarm summary shape (LoRa)
- alarm code
- severity
- source short id
- value short
- battery short
- wake cause bits
- coarse timestamp delta

---

## 8. Codec families

この repo では codec family を明示します。

- `state_full_v1`
- `state_compact_v1`
- `state_summary_v1`
- `event_summary_v1`
- `command_stub_v1`
- `command_result_compact_v1`
- `heartbeat_summary_v1`

`summary codec がない message` は LoRa fallback しない。

---

## 9. Message classes and suggested body sizes

| Message class | Preferred bearer | Body target |
|---|---|---:|
| critical alert summary | LoRa direct/relay | 4–10 B |
| sparse telemetry summary | LoRa direct/relay | 6–20 B |
| command stub | LoRa downlink only if tiny | 4–12 B |
| command result compact | LoRa or Wi-Fi mesh | 4–20 B |
| detailed telemetry | Wi-Fi | 32–256 B |
| diagnostics | Wi-Fi | 64–512 B |
| file chunk | Wi-Fi only | 256–1024 B+ |

---

## 10. Important consequence

LoRa mesh を入れた結果、
**direct では送れた summary でも relay を通すと入らなくなる** 場合があります。

だから route decision は payload size を見なければならない。

例:
- direct overhead 14 B, cap 24 B → app body 10 B
- relay overhead 18 B, cap 24 B → app body 6 B

この差は大きい。

---

## 11. Default rules

### RULE-1
JSON を LoRa on-wire にそのまま載せない。

### RULE-2
LoRa summary packet は human-readable である必要はない。

### RULE-3
relay path を許す class は summary codec を持つこと。

### RULE-4
same logical message に full / compact / summary の 3 shape があってよい。

### RULE-5
payload design は radio の最後で考えるのではなく、message schema の時点で考える。

---

## 12. What Codex should implement first

1. short id lease
2. compact envelope struct
3. direct LoRa overhead estimator
4. relayed LoRa overhead estimator
5. summary codec registry
6. over-cap decision helper
