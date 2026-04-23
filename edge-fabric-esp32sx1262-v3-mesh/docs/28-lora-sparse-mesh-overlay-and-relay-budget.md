# LoRa Sparse Mesh Overlay And Relay Budget

## 1. まず大前提

LoRa 側の mesh は、**Wi-Fi mesh の代わりの主骨格**ではありません。  
この repo では LoRa mesh を **疎な長距離 overlay** として定義します。

つまり、
- direct で届くなら direct
- relay は必要な edge だけ
- payload は tiny / summary
- hop は保守的
です。

---

## 2. Why constrained mesh

### 2.1 LoRaWAN is not mesh
LoRaWAN は star-of-stars で、end-device は single-hop です。 `[S23][S24][S25]`

### 2.2 raw LoRa mesh is possible, but not free
custom store-and-forward は作れるが、
- airtime
- collision
- duty headroom
- relay listen cost
- downlink timing
が効いてきます。

### 2.3 battery relay is a trap
relay whitepaper でも relay の消費は listen が大きいと説明されています。 `[S17]`

---

## 3. Standard LoRa overlay roles

### sleepy_leaf
source only, no relay

### lora_relay
always-on relay

### dual_bearer_bridge
LoRa ↔ Wi-Fi domain bridge

### gateway_head
LoRa ingress head

---

## 4. Standard hop policy

### direct
source -> gateway/bridge

### 1-relay
source -> relay -> gateway/bridge

### 2-relay
source -> relay A -> relay B -> gateway/bridge

### 3-relay
experimental only

repo default:
- sleepy leaf: direct or 1-relay preferred
- critical exception: 2-relay allowed
- 3-relay: explicit site policy only

---

## 5. Relay duties

relay は次を担当する。

- tiny beacon
- neighbor table
- store-and-forward
- hop ack
- duplicate suppression
- hop limit enforcement
- queue pressure export
- duty headroom export

relay が担当しないもの:
- bulk file relay
- large telemetry log
- site-wide durable state
- business logic

---

## 6. Beacon policy

relay beacons は very small でなければならない。

含める候補:
- relay short id
- current depth to egress
- route cost short
- queue pressure short
- duty headroom short
- profile bits
- epoch / lease generation short

interval guidance:
- normal: 60–180 s + jitter
- degraded recovery: temporarily shorter
- sleepy nodes are not expected to listen continuously

---

## 7. Neighbor table fields

- short id
- last seen age
- RSSI bucket
- SNR bucket
- success rate
- advertised depth
- route cost
- queue pressure
- duty headroom
- relay capability flags

---

## 8. Route selection for LoRa overlay

route score should include:
- hop count
- link success
- relay load
- last-good success
- payload fit
- priority
- current duty headroom
- whether target is sleepy

route goals:
- shortest good path
- avoid flapping
- avoid overloaded relay
- avoid needless extra hops

---

## 9. Duplicate and loop control

LoRa mesh で最も怖いのは loop と duplicate storm です。  
そのため、relay は必ず:

- `message_id` / short flow id cache
- `hop_limit`
- `seen recently` suppression
- `origin session + seq` observation
を持つ。

---

## 10. Relay budget

relay には予算が要ります。

### budget dimensions
- max queue bytes
- max relayed packets / interval
- max beacon overhead
- max allowed priority downgrade
- duty headroom threshold
- degraded threshold

### default policy
queue pressure が高い時は:
1. bulk not allowed anyway
2. normal summary を間引く
3. control はなるべく保持
4. critical は最後まで保持

---

## 11. Ack strategy on LoRa mesh

### hop ack
next hop accepted into queue

### end-to-end persist ack
Site Router persisted

### sleepy leaf reality
persist ack は immediate ではなく next RX window / next poll になることがある

---

## 12. What LoRa mesh is good for

- battery alerts
- sparse telemetry
- remote outbuilding summary
- fallback when Wi-Fi path is lost
- bridge between isolated islands

## 13. What LoRa mesh is bad for

- actuator closed loop
- chatty telemetry
- verbose logs
- image/file
- OTA
- multi-hop human chat style traffic

---

## 14. Long distance strategy

“長距離でも使える” を現実的にやるには、
- edge battery leaves
- powered relay boxes
- dual_bearer bridges
- backbone Wi-Fi/IP domains
の組み合わせが効きます。

つまり、LoRa だけで全部を長距離メッシュするのではなく、  
**LoRa で遠くの edge を拾って、そこから先は Wi-Fi/IP backbone に載せる** のが基本戦略です。
