# Validation Plan And Open Questions

## 1. Validation philosophy

この repo は “mesh が動く” だけでは不十分です。  
次の 4 つを個別に検証する必要があります。

1. **動くか**
2. **壊れたときに復旧するか**
3. **battery を食い潰さないか**
4. **payload / airtime / queue が現実に収まるか**

---

## 2. Test matrix

## 2.1 Single gateway starter
- Ubuntu server 1
- USB gateway head 1
- sleepy leaf 5
- powered leaf 3

見るもの:
- join
- uplink / downlink
- persist ack
- duplicate suppression
- summary codec

## 2.2 Wi-Fi mesh backbone
- mesh_root 1
- mesh_router 3
- powered_leaf 10

見るもの:
- self-organized parent formation
- root connected state
- parent loss and reselection
- route flapping suppression
- queue growth when upstream stalls

## 2.3 LoRa sparse relay
- direct leaf 4
- lora_relay 2
- dual_bearer_bridge 1
- gateway_head 1

見るもの:
- direct vs relay route choice
- hop ack
- end-to-end ack
- relay dedupe cache
- hop limit
- relay queue pressure

## 2.4 Hybrid redundant alert
- powered leaf 1
- Wi-Fi mesh path available
- LoRa path available

見るもの:
- same `event_id` duplicated over both bearers
- Site Router dedupe
- ack settlement
- route metrics update

## 2.5 Sleepy leaf
- periodic wake leaf
- event wake leaf
- maintenance_awake path

見るもの:
- wake cost
- last-good parent list
- bounded scan time
- next-poll command delivery
- downlink receive window timing

## 2.6 Multi-domain site
- mesh_root A / B
- gateway_head 2
- Host Agent 2
- Site Router 1
- controller client 2

見るもの:
- ingress merge
- domain crossing
- targeted host/client routing
- command arbitration
- logical writer singularity

---

## 3. Acceptance criteria

### A. battery safety
- sleepy leaf に relay duty が割り当たらない
- maintenance_awake を除き Wi-Fi aggressive scan をしない
- heartbeat 単独 packet が過剰に増えない

### B. payload discipline
- LoRa over-cap payload が silent drop されない
- summary codec か Wi-Fi defer に落ちる
- route class ごとの cap が守られる

### C. mesh behavior
- Wi-Fi parent failure から復帰する
- root failure 時に domain が回復するか、少なくとも degraded として検出できる
- LoRa relay failure 時に alternate path or degraded fallback が働く

### D. multi-ingress correctness
- same event の multi-path duplicate が dedupe される
- command は 1 つの logical writer から整列される
- secondary controller が direct node transport を持たない

---

## 4. Measurement harness

## 4.1 radio metrics
- Wi-Fi RSSI
- LoRa RSSI / SNR
- relay success rate
- retry counts
- queue depth

## 4.2 power metrics
- wake duration
- TX count / day
- RX window count / day
- maintenance_awake duration
- estimated battery drain

## 4.3 storage metrics
- NVS write frequency
- flash sector churn
- queue coalescing ratio
- oldest unsent age

## 4.4 route metrics
- chosen route class
- chosen next hop
- hop count
- reason code for route switch
- hysteresis state

---

## 5. Open questions to keep explicit

### Q1
Wi-Fi mesh root を完全自動 election にするか、site policy で designated root 優先にするか。

### Q2
LoRa relay beacon の interval を何秒にするか。  
短すぎると無駄、長すぎると recovery が遅い。

### Q3
LoRa relay default hop cap を 2 に固定するか、role lease で zone ごとに緩めるか。

### Q4
Wi-Fi LR mode を v1.1 optional backhaul として入れるか。 `[S9]`

### Q5
active/standby の Site Router をいつ入れるか。  
v1 は single logical writer のままでよいが、運用上の HA は将来欲しい。

### Q6
relay queue saturation 時、どの priority をいつ捨てるか。  
今は `bulk > normal > control > critical` の順に落とす方針。

### Q7
mesh root と gateway head の機能をどこまで共通化するか。  
Wi-Fi mesh root の firmware と USB radio head の firmware を分けるか、共通 runtime にするか。
