# Reference Deployments And Device Patterns

## 1. この文書の目的

“この基盤で何ができるのか” を、device パターンごとに具体化します。

---

## 2. Pattern A: ultra-low-power battery event node

### 例
- PIR
- door contact
- leak sensor
- vibration alarm

### role
`sleepy_leaf`

### primary path
LoRa direct / 1-relay

### command expectation
eventual / next-poll

### notes
- relay しない
- heartbeat は piggyback
- maintenance だけ Wi-Fi

---

## 3. Pattern B: periodic battery telemetry node

### 例
- temp/humidity
- tank level
- meter pulse aggregation

### role
`sleepy_leaf`

### primary path
LoRa direct

### notes
- periodic wake
- compact summary
- interval command only
- no routine Wi-Fi attach

---

## 4. Pattern C: powered actuator node

### 例
- servo controller
- relay board
- valve driver
- display controller

### role
`powered_leaf`

### primary path
Wi-Fi mesh / Wi-Fi IP

### notes
- immediate command target
- closed-loop remains local
- LoRa summary only for fallback/alarm

### important
“サーボをこの fabric で扱う” は可能。  
ただし fabric が送るのは:
- setpoint
- mode
- stop
- state
- fault
であって、PWM のミリ秒単位制御ではない。

---

## 5. Pattern D: bridge box

### 例
- remote building bridge
- rooftop bridge enclosure
- outdoor powered pole

### role
`dual_bearer_bridge`

### primary path
Wi-Fi mesh locally, LoRa upstream or sideways

### notes
- very valuable in hybrid sites
- good place to aggregate edge alerts
- can expose local maintenance path

---

## 6. Pattern E: LoRa relay box

### 例
- remote repeater with mains power
- protected enclosure with stable power

### role
`lora_relay`

### notes
- always_on only
- tiny beacon only
- sparse relay only
- not a general bulk repeater

---

## 7. Pattern F: mesh root / building root

### 例
- building comm closet node
- server-room adjacent root
- floor gateway controller

### role
`mesh_root`

### notes
- domain root
- upstream to Site Router
- local backbone head
- can coexist with controller software nearby

---

## 8. Pattern G: Ubuntu controller client

### 例
- analytics worker
- automation service
- site UI
- maintenance console

### role
`controller_client`

### notes
- subscribes to services
- sends commands via Site Router
- can host additional Host Agent
- not the durable writer by default

---

## 9. Pattern H: single Ubuntu + many ESPs

これはユーザーが最初に想像している典型構成です。

- Ubuntu 1 台
- USB gateway 1 台
- powered mesh domain 1
- sleepy battery nodes dozens
- control nodes handful

この repo では fully supported target pattern です。

---

## 10. Pattern I: two Ubuntu servers + many nodes

- Ubuntu A: Site Router + UI
- Ubuntu B: automation + extra Host Agent
- gateways / roots distributed
- nodes subscribe by service
- commands go through Site Router

これも supported target pattern です。

---

## 11. Pattern J: long-range remote outbuilding

- building A has mesh domain
- outbuilding has bridge box
- outbuilding local devices use Wi-Fi local
- bridge upstream uses LoRa sparse overlay

これが “Wi-Fi と LoRa を最適に使い分ける” 典型です。

---

## 12. Pattern K: human presence immediate alert

- local PIR event
- local buzzer/light immediately
- Wi-Fi immediate to nearby control room
- LoRa summary to remote edge / fallback server
- Site Router logs event once via dedupe

これが redundant critical path の典型です。
