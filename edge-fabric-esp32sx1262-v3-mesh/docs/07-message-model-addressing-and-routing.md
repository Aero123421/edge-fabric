# Message Model, Addressing, And Routing

## 1. Message model の基本方針

transport より **意味** を先に固定する。  
この repo でアプリが扱う message kind は次です。

- `state`
- `event`
- `command`
- `command_result`
- `heartbeat`
- `manifest`
- `lease`
- `file_chunk`
- `fabric_summary`

---

## 2. Envelope の基本形

論理 envelope は少なくとも以下を持つ。

```json
{
  "schema_version": "1.0.0",
  "message_id": "msg-001",
  "kind": "event",
  "priority": "critical",
  "event_id": "evt-001",
  "source": {
    "hardware_id": "hw-001",
    "session_id": "sess-001",
    "seq_local": 42,
    "fabric_short_id": 201
  },
  "target": {
    "kind": "service",
    "value": "alerts"
  },
  "delivery": {
    "route_class": "critical_alert",
    "allow_relay": true,
    "allow_redundant": true,
    "hop_limit": 2
  },
  "payload": {}
}
```

---

## 3. message_id / event_id / command_id

### message_id
配送単位。

### event_id
同じ実世界イベントを束ねる単位。  
Wi-Fi と LoRa に redundant send しても same `event_id` を使う。

### command_id
同じ操作意図を束ねる単位。  
retry でも変えない。

---

## 4. state / event / command の意味

### state
今どうなっているか。

### event
何が起きたか。

### command
何をしてほしいか。

### command_result
それがどうなったか。

これを分けることで、
- latest state
- append-only event ledger
- command lifecycle
を別々に扱える。

---

## 5. Addressing model

target は以下を表せる。

- `node:<id>`
- `group:<id>`
- `service:<name>`
- `host:<id>`
- `client:<id>`
- `site:<id>`
- `broadcast`

### 推奨
node app は通常、
- `service`
- `group`
- `node`
を主に使う。

### host / client
“この Ubuntu server のこの client に返したい” は Site Router 側で解決する。

---

## 6. Routing metadata

message は optional に以下を持てる。

- `route_class`
- `allow_relay`
- `allow_redundant`
- `hop_limit`
- `mesh_domain_id`
- `hop_count`
- `ingress_gateway_id`
- `last_hop`

これにより message semantics と route policy を分ける。

---

## 7. Ordering and idempotency

ordering の主軸:
- `occurred_at`
- `session_id`
- `seq_local`

idempotency の主軸:
- same real-world event → same `event_id`
- same operator action → same `command_id`

---

## 8. Why short IDs are needed

LoRa summary / relay では payload が厳しい。  
そのため hardware_id の代わりに `fabric_short_id` を on-wire で使えるようにする。

- long identity: `hardware_id`
- site binding: `logical_binding_id`
- compact on-wire: `fabric_short_id`

---

## 9. Message shape separation

1 つの logical message に対して、
- full shape
- compact shape
- summary shape
を持てる。

例:
- `event_full_v1` over Wi-Fi
- `event_summary_v1` over LoRa

---

## 10. Routing principle

アプリは “どの bearer を使うか” を言わない。  
fabric は “どの route class か” を見て path を決める。

例:
- `critical_alert` → LoRa and/or Wi-Fi
- `bulk_only` → Wi-Fi only
- `immediate_local` → Wi-Fi local only
