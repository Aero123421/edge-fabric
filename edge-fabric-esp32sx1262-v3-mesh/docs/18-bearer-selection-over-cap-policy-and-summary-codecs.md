# Bearer Selection, Over-Cap Policy, And Summary Codecs

## 1. bearer selection を “route class” で行う

アプリが指定するのは bearer ではなく、message の意味と urgency です。  
fabric はそこから route class を決めます。

### route classes
- `immediate_local`
- `normal_state`
- `critical_alert`
- `sparse_summary`
- `bulk_only`
- `maintenance_sync`

---

## 2. Default mapping

| Route class | Default path | Fallback | Notes |
|---|---|---|---|
| immediate_local | Wi-Fi mesh / Wi-Fi IP | none | low-latency only |
| normal_state | Wi-Fi mesh / Wi-Fi IP | LoRa summary only if codec exists | powered default |
| critical_alert | Wi-Fi + LoRa redundant or LoRa direct | degraded allowed | dedupe required |
| sparse_summary | LoRa direct / relay | another relay or queue | sleepy default |
| bulk_only | Wi-Fi only | queue/defer only | no LoRa |
| maintenance_sync | Wi-Fi only | none | service mode only |

---

## 3. Node-type based default

### sleepy_leaf
- default class: `sparse_summary`
- event: `critical_alert` if configured
- no `bulk_only`
- `maintenance_sync` only in maintenance_awake

### powered_leaf
- default class: `normal_state`
- control: `immediate_local`
- `critical_alert`: redundant optional

### mesh_router / mesh_root
- infra traffic: `normal_state`
- root health: compact Wi-Fi first
- no user bulk on relay queues

### lora_relay
- relay control and beacons: tiny LoRa internal class
- no app bulk

---

## 4. Over-cap policy

LoRa path を選んだが payload が cap を超えた時は、次の順序で処理する。

### STEP-1
summary codec があるか確認する。

### STEP-2
summary で入るなら summary shape に落とす。

### STEP-3
summary でも入らないなら、
- Wi-Fi path available → Wi-Fi へ回す
- Wi-Fi path unavailable → queue / defer / reject reason を返す

### STEP-4
silent drop はしない。

---

## 5. Summary codec design

summary codec は「雑に truncate するもの」ではありません。  
**意味を保って圧縮するもの**です。

### 例
- detailed sensor report  
  → latest value + severity + battery + coarse age
- control status detail  
  → mode + success/fail + short reason code
- alarm detail  
  → alarm code + severity + source short id + compact sample

---

## 6. Redundant send policy

critical alert は同じ `event_id` で、
- Wi-Fi full/compact
- LoRa summary
の両方を送ってよい。

必須条件:
- `event_id` same
- dedupe at Site Router
- arrival order independent
- ack settlement on logical event basis

---

## 7. Hysteresis

route choice は毎 packet 揺らしてはいけない。  
この repo では hysteresis を持つ。

### inputs
- Wi-Fi parent stability
- Wi-Fi RTT / loss
- LoRa SNR / last success
- relay load
- queue age
- battery policy
- wake class
- payload size

### outputs
- stay on current path
- switch path
- duplicate current critical
- queue and defer

---

## 8. Immediate local must stay local

`immediate_local` の代表:
- servo angle set
- relay on/off for control loop
- local display update
- operator interactive control

これらは Wi-Fi mesh / Wi-Fi IP / wired local path を使う。  
LoRa へ逃がさない。

---

## 9. LoRa fallback criteria

LoRa fallback を許可してよいのは次の条件を満たす時だけ。

1. summary codec exists
2. payload fits route profile
3. node role allows LoRa
4. current duty headroom allows
5. hop count within policy
6. latency expectation is not immediate

---

## 10. Degraded modes

### degraded-wifi
Wi-Fi mesh が不安定。  
summary を LoRa に落として継続。

### degraded-lora
LoRa relay path が不安定。  
critical だけ送る / normal は queue。

### local-only
upstream 不通。  
Site Router か local controller で保持し、後で同期。

---

## 11. Codex helper modules

- `RouteClassResolver`
- `PayloadSizer`
- `SummaryCodecRegistry`
- `OverCapPolicy`
- `PathHysteresisState`
- `RedundantSendPlanner`
