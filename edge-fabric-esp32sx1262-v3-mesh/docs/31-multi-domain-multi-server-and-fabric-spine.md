# Multi-Domain, Multi-Server, And Fabric Spine

## 1. ユーザー要求をそのまま言い換える

この repo は、次のような site を扱いたい。

- Ubuntu server 1 台でも動く
- Ubuntu server 2 台以上でも動く
- USB gateway が複数あってよい
- Wi-Fi mesh root が複数あってよい
- 遠い edge は LoRa relay で拾いたい
- 複数の controller app から観測・制御したい
- でも command / state の正本は壊したくない

これを実現する鍵が **Fabric Spine** です。

---

## 2. Fabric Spine とは何か

root / gateway / server / controller client をつなぐ上位の transport 層です。  
具体的には:
- local IP
- Ethernet
- USB host link
- VPN
- special backhaul

LoRa や Wi-Fi mesh の radio plane と分けて考える。

---

## 3. Why spine is needed

ESP FAQ では、no-router scenario の multiple root nodes は直接メッセージを送れないとされています。 `[S22]`  
つまり、
- domain A root
- domain B root
がそれぞれ存在しても、radio mesh だけでは site 全体が 1 つになるとは限らない。

だから上位の spine で束ねる。

---

## 4. Recommended multi-server model

### 4.1 one active logical writer
Site Router は 1 つ active。

### 4.2 many controller clients
Ubuntu server A / B / C は client として参加できる。

### 4.3 many ingress points
USB gateway / mesh root / bridge は複数あってよい。

### 4.4 optional standby
将来 active/standby は可能だが、v1 default は active one.

---

## 5. Example architectures

## 5.1 single Ubuntu starter

```text
Ubuntu(Server + Site Router)
  ├─ USB Gateway Head
  └─ Mesh Root A
```

## 5.2 dual Ubuntu control

```text
Ubuntu A (Site Router + Controller)
Ubuntu B (Controller + Host Agent)
  ├─ USB Gateway Head B
  └─ Mesh Root B
```

## 5.3 campus site

```text
Site Router (server room)
  ├─ Host Agent A -> Gateway A
  ├─ Host Agent B -> Gateway B
  ├─ Domain Root A
  ├─ Domain Root B
  ├─ Domain Root C
  └─ Controller Clients (ops / analytics / automation)
```

---

## 6. Addressing in multi-server world

node から見えるのは基本的に:
- `service`
- `group`
- `node`

必要に応じて Site Router が:
- `host`
- `client`
へ配送する。

これにより node は “server A に送れ” を知らなくてよい。

---

## 7. Multi-domain crossing

domain A の powered node から domain B の controller へ message を届ける時:

1. source node -> domain A root
2. root -> Fabric Spine
3. Site Router ingest
4. Site Router route
5. destination client / host / domain B root
6. if needed downlink to domain B node

domain 跨ぎは **Site Router aware** にする。

---

## 8. Local-only operation

spine が落ちても、site の一部は local-only で続けられる。

例:
- building A domain 内 control は継続
- controller client A だけは見える
- upstream summary は queue
- later sync

この local-only mode を health に出す。

---

## 9. What multiple servers can do

### yes
- observe same site
- subscribe same events
- send commands through Site Router
- host their own local apps
- attach extra gateways/roots

### no by default
- each independently become durable writer
- directly race commands to nodes
- bypass Site Router and still expect coherent state

---

## 10. Why this is still “generic”

この設計は KGuard 専用ではありません。  
factory, farm, warehouse, campus, remote facility でも同じです。

違うのは上の app だけで、
- mesh domains
- sparse LoRa overlay
- site router
- controller clients
という骨格は共通です。
