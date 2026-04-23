# Scale Profiles, Capacity Planning, And Hop Guidance

## 1. 重要な注意

“何台までいけるか” は、**台数そのもの**より **traffic profile** に強く依存します。  
この文書の数字は保証値ではなく、**設計の目安**です。

---

## 2. Why scale is not one number

100 台でも、
- 1 分に 1 回 tiny summary だけ
と
- 1 秒に数回 command / telemetry を打つ
では全く別です。

しかもこの repo では、
- Wi-Fi mesh
- LoRa direct
- LoRa relay
- Fabric Spine
が混在するので、site 全体を 1 数字で表すのは危険です。

---

## 3. Scale profiles

## S0: starter
- total devices: 5–20
- server: 1
- gateway: 1
- roots: 0–1
- LoRa relay: 0
- battery leaf: 少数
- powered leaf: 少数

向き:
- PoC
- lab
- single room

## S1: small site
- total devices: 20–80
- roots: 1–2
- gateways: 1–2
- LoRa relay: 0–2
- battery edge: 10–30
- powered backbone/control: 10–40

向き:
- small warehouse
- small factory line
- building floor

## S2: medium site
- total devices: 80–200
- roots: 2–4
- gateways: 2–4
- LoRa relay: 2–6
- multiple controller clients

向き:
- building complex
- campus subset
- farm blocks

## S3: large-ish edge site
- total devices: 200–400+
- roots: 4+
- gateways: 4+
- multiple domains
- multiple controllers
- LoRa used only for sparse edge and fallback

向き:
- campus
- plant sections
- multi-building facility

---

## 4. Backbone rule at scale

台数が増えるほど、主骨格は
- Wi-Fi mesh
- local IP
- Ethernet / Fabric Spine
に寄せる。

LoRa は scale の主役ではなく、**site edge coverage** の主役です。

---

## 5. Hop guidance

### Wi-Fi mesh
API 上の max layer は大きく取れるが、repo 推奨は shallow design。

- recommended depth: 3–5
- 6+ is tuned deployment
- control-heavy islands should be shallower

### LoRa
- direct first
- 1-relay common upper edge
- 2-relay restricted
- 3-relay experimental

---

## 6. Device mix guidance

### good mix
- many sleepy leaves
- modest number of powered routers/roots
- a few relays/bridges
- one durable Site Router

### bad mix
- too many always-on LoRa relays
- all nodes trying to be dual-bearer
- battery nodes acting as routers
- large payloads trying LoRa fallback

---

## 7. Capacity planning checklist

- how many sleepy event nodes?
- how many periodic telemetry nodes?
- how many powered command targets?
- how many domains?
- how many buildings?
- how many critical alerts / hour?
- how many large logs / day?
- any servo / motor local control islands?
- how many gateways / roots can you power?

---

## 8. Design heuristics

### heuristic A
LoRa path should carry **small percentage of total bytes**, not majority.

### heuristic B
Wi-Fi mesh domain should be shaped around physical topology.

### heuristic C
relay count should be minimized.

### heuristic D
deep sleep nodes should be numerous; relays should be few.

### heuristic E
if a path needs many hops and large payloads, redesign the topology.

---

## 9. Scaling down is also important

この repo は scale up だけでなく scale down も重要視する。

最小構成:
- Ubuntu 1
- USB gateway 1
- sleepy leaf 3
- powered leaf 2
でも同じ contract で動くべき。

ここが generic foundation として重要。
