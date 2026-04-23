# Wi-Fi Bearer Acquisition, Maintenance, And Hybrid Policy

## 1. この文書の目的

Wi-Fi は “速いから全部 Wi-Fi” ではなく、  
**誰がどの mode で、どのくらい attach / roam / stay するか** を決めないと破綻します。

---

## 2. Wi-Fi bearer families

### 2.1 wifi_ip
普通の IP 接続。  
router/AP がある site 向け。

### 2.2 wifi_mesh
ESP-WIFI-MESH backbone。  
powered backbone の主力。 `[S20][S21]`

### 2.3 wifi_lr
optional long-range backhaul candidate。  
v1 mandatory ではない。 `[S9]`

### 2.4 local_ap_maintenance
保守用 SoftAP or APSTA path。  
commissioning / diagnostics 用。

---

## 3. Acquisition policy by role

### sleepy_leaf
- deployed mode: acquire しないのが標準
- maintenance_awake: acquire 許可
- note: Wi-Fi 連続 attach を前提にしない

### powered_leaf
- acquire on boot
- keep attached
- parent/root changes allowed under hysteresis

### mesh_router
- mesh attach mandatory
- parent reselection allowed

### mesh_root
- mesh root responsibilities
- upstream IP/host state monitored
- channel / router state exported

### gateway_head
- local Wi-Fi optional
- primary host link is USB

---

## 4. Why sleepy nodes should avoid routine Wi-Fi acquisition

- sleep で Wi-Fi connection は維持されない `[S15]`
- scan / attach / DHCP / TLS などは wake budget を食う
- network conditions により wake time が読みにくくなる
- periodic tiny telemetry には重い

したがって、sleepy leaf は LoRa-first / maintenance Wi-Fi とする。

---

## 5. Wi-Fi mesh practical rules

### RULE-1
mesh domain ごとに root を明示する。

### RULE-2
building / floor / zone 単位で domain 分割を検討する。

### RULE-3
mesh root の upstream が不安定なら domain health を degraded にする。

### RULE-4
route switch は hysteresis を持つ。

### RULE-5
controller traffic は local IP / mesh root 経由で Site Router に集約する。

---

## 6. Hybrid policy examples

### Example A: powered control node
- command in: Wi-Fi mesh
- state out: Wi-Fi mesh
- critical alarm: Wi-Fi + LoRa summary optional

### Example B: battery PIR
- event out: LoRa direct / relay
- heartbeat: piggyback
- config: next-poll tiny command
- maintenance: Wi-Fi only when serviced

### Example C: bridge node in remote building
- local children: Wi-Fi mesh
- upstream to campus: LoRa relay or Wi-Fi LR/IP
- acts as dual_bearer_bridge

---

## 7. Wi-Fi path health

健康度に使う値:
- attach state
- parent age
- root reachable
- toDS backlog
- RTT / loss
- domain change count
- recent route flaps

これらは route decision に渡される。

---

## 8. APSTA / local maintenance

APSTA は便利だが、channel coupling を理解して使う。  
maintenance AP は常時ではなく、必要時だけ立てる。

---

## 9. v1 recommendation

### mandatory
- `wifi_ip`
- `wifi_mesh`

### optional
- `wifi_lr`
- `local_ap_maintenance`

v1 では、まず Wi-Fi mesh backbone をちゃんと安定させることを優先する。
