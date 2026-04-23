# Hybrid Routing Cost Model And Path Classes

## 1. Why a cost model is needed

hybrid fabric で “勝手に最適化” をやるには、  
単なる `if wifi else lora` では足りません。

必要なのは、
- その message の意味
- payload が入るか
- その node が何者か
- 今ネットワークがどれだけ元気か
- relay を増やす価値があるか
をまとめて点数化することです。

---

## 2. Route classes

### immediate_local
最優先で local / low-latency。

### normal_state
通常状態。Wi-Fi primary。

### critical_alert
重要アラート。LoRa と Wi-Fi の冗長を許可。

### sparse_summary
LoRa primary の summary。

### bulk_only
Wi-Fi only.

### maintenance_sync
保守時だけの rich transfer.

---

## 3. Candidate path families

### P1: wifi_mesh_direct
powered local path.

### P2: wifi_mesh_via_root
domain 上流 path.

### P3: lora_direct
sleepy edge or sparse summary.

### P4: lora_relay_1
one relay path.

### P5: lora_relay_2
two relay path.

### P6: redundant_wifi_plus_lora
critical only.

### P7: queue_and_defer
no good path now.

---

## 4. Cost terms

route cost は次のような項を持つ。

- bearer base cost
- hop penalty
- airtime penalty
- payload fit penalty
- relay load penalty
- duty risk penalty
- stale-link penalty
- wake-class mismatch penalty
- latency mismatch penalty
- battery policy penalty

---

## 5. Design intuition

### immediate_local
Wi-Fi が圧倒的に有利。  
LoRa は基本禁止。

### sparse_summary
LoRa direct が有利。  
Wi-Fi attach cost は高い。

### critical_alert
redundant に価値がある。  
特に powered critical source では Wi-Fi full + LoRa summary が強い。

### bulky telemetry
LoRa は論外。  
defer or Wi-Fi.

---

## 6. Hysteresis

route を切り替えるには threshold を超える必要がある。  
また、切り替えた直後は hold-down を置く。

理由:
- Wi-Fi parent flap
- temporary LoRa fade
- queue oscillation
で経路が踊るのを防ぐため。

---

## 7. Example policy table

| Source type | Message kind | Default route class | Preferred path |
|---|---|---|---|
| sleepy PIR | event | critical_alert | LoRa direct/relay + optional local redundant |
| sleepy meter | state | sparse_summary | LoRa direct |
| powered display | state | normal_state | Wi-Fi mesh |
| servo controller | command_result | immediate_local | Wi-Fi mesh/IP |
| diagnostics blob | file_chunk | bulk_only | Wi-Fi only |
| bridge health | heartbeat | normal_state | Wi-Fi, LoRa summary optional |

---

## 8. Failover policy

### Wi-Fi degraded
- normal_state may drop to summary over LoRa if codec exists
- immediate_local does not fail over to LoRa
- bulk_only queues

### LoRa degraded
- sparse_summary queues or drops normal low-priority
- critical_alert may try alternate relay
- powered node may prefer Wi-Fi only

---

## 9. Redundant send policy

only for:
- critical alarm
- critical command result
- maybe root health emergency

never for:
- bulk
- high-rate state stream
- ordinary heartbeat spam

---

## 10. Route decision output

route decision should return:
- chosen path class
- chosen next hop / ingress set
- whether redundant
- whether summary transformed
- ack expectation
- defer reason if queued

---

## 11. What developers see

開発者には route cost 式を見せなくてもよい。  
ただし SDK には最低限:
- priority
- message kind
- delivery expectation
- maybe latency class
を渡せる必要がある。

つまり開発者は “意味” を宣言する。  
fabric は “経路” を決める。
