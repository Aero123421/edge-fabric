# Identity, Provisioning, And Security

## 1. Identity layers

### immutable identity
- `hardware_id`
- `gateway_id`
- `host_id`
- `client_id`

### mutable site binding
- `site_id`
- `logical_binding_id`
- `mesh_domain_id`
- `role_lease_id`

### compact on-wire identity
- `fabric_short_id`

この 3 層を分ける。

---

## 2. Why short IDs matter

LoRa summary / relay path では payload が厳しいため、  
on-wire では `fabric_short_id` を使えるようにする。

注意:
- short id は site-local
- hardware_id の代わりではない
- lease で再配布されうる

---

## 3. Session and ordering

- `session_id`: boot/reconnect 境界
- `seq_local`: same session 内の単調増加
- `message_id`: 配送単位
- `event_id`: 実イベント単位
- `command_id`: 実操作意図単位

---

## 4. Provisioning flow

1. device boots
2. emits `manifest`
3. site evaluates capability
4. site returns `lease`
5. node stores role / short id / preferred parents / route profile

---

## 5. Manifest should include

- hardware identity
- power class
- wake class
- supported bearers
- allowed roles
- relay capability
- command support
- firmware versions

## 6. Lease should include

- effective role
- fabric short id
- mesh domain id
- primary/fallback bearer
- preferred roots / gateways / relays
- hop budget
- reporting profile
- heartbeat profile
- maintenance policy

---

## 7. Security stance

- QR に long-term secret を直書きしない
- bearer-level protection だけに依存しない
- fabric envelope にも protection をかけられるようにする
- revoke / rotate / decommission を持つ

---

## 8. Authorization idea

- node auth: site enrollment
- client auth: local user/service auth
- controller app は command 権限を role-based に持てる
- direct host/client routing は authz 対象

---

## 9. Mesh-aware provisioning notes

### sleepy leaf
- parent hints を少数だけ配る
- aggressive discovery を前提にしない

### mesh router / root
- domain id を明示
- relay role と root role を分ける

### dual_bearer_bridge
- both side capabilities
- path class restrictions
- beacon behavior
