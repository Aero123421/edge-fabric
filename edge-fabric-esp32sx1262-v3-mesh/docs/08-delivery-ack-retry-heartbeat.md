# Delivery, ACK, Retry, And Heartbeat

## 1. Delivery model

v1 の delivery model は **at-least-once + dedupe + idempotency** です。  
LoRa relay, Wi-Fi mesh, USB ingress, multiple roots/gateways が絡むため、 exactly-once は狙いません。

---

## 2. ACK levels

### Hop ACK
次ホップが queue に受けたこと。  
LoRa relay と mesh forwarding で使う。

### Persist ACK
Site Router が durable に保存したこと。

### Command Phase ACK
`issued -> accepted -> executing -> succeeded / failed / rejected / expired`

### Optional App ACK
必要時のみ。

---

## 3. Priority

- `critical`
- `control`
- `normal`
- `bulk`

### critical
drop しない / durable / redundant 可

### control
command 系 / durable / immediate path 優先

### normal
coalesce 可 / summary 可

### bulk
Wi-Fi only / resumable

---

## 4. Retry layers

### radio retry
短い retry。  
hop-level。

### queue retry
node / relay / Site Router queue からの再送。

### logical retry
operator action or higher-level replay。

これらを混同しない。

---

## 5. Heartbeat layers

- Node heartbeat
- Relay heartbeat
- Mesh root heartbeat
- Gateway heartbeat
- Host Agent heartbeat
- Fabric summary heartbeat

---

## 6. Health states

- `live`
- `stale`
- `offline`
- `degraded`
- `local_only`
- `isolated`
- `blocked`

---

## 7. Special rules for sleepy nodes

- heartbeat は piggyback 優先
- persist ack は next RX window / next poll になりうる
- immediate control ACK は期待しない

---

## 8. Multi-path duplicate

same `event_id` が
- Wi-Fi
- LoRa
- gateway A
- gateway B
から来てもよい。  
Site Router が dedupe する。

---

## 9. Dead-letter

queue policy 上これ以上 retry できないものは dead-letter へ。  
ただし critical は簡単に silent drop しない。
