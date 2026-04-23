# Wi-Fi Mesh Backbone, Root, And Domain Policy

## 1. Why Wi-Fi mesh is the backbone

powered node の世界では、LoRa より Wi-Fi の方が
- payload に余裕がある
- low-latency command に向く
- diagnostics / logs を流しやすい
- local server と統合しやすい

したがって、この repo は **Wi-Fi mesh を主骨格**にします。

---

## 2. ESP-WIFI-MESH facts that matter

Espressif の guide では、ESP-WIFI-MESH は **self-organizing / self-healing** と説明されています。 `[S20]`

API / guide 上で重要なのは次です。 `[S21]`

- root を持つ
- parent / child 構造を持つ
- external IP network へ出るには root が `toDS` を扱う
- max layer を設定できる
- fixed channel を前提にする
- max_connection は softAP 側の制約を受ける
- root conflict は同じ BSSID なら処理できるが、違う BSSID はアプリ側 forward が必要

---

## 3. Domain concept

この repo では、ESP-WIFI-MESH の 1 ネットワークを **mesh domain** と呼びます。

domain の主な属性:
- `mesh_domain_id`
- `mesh_id`
- `root_policy`
- `max_layer`
- `upstream_kind`
- `channel_policy`
- `domain_priority`

---

## 4. Root policy

### 4.1 self-organized root
ESP-WIFI-MESH の自動 root 選出を使う。 `[S20]`

向く場面:
- 単一 building
- power-on が比較的そろう
- topology を柔軟にしたい

注意:
- no-router multiple root 問題
- root の位置が変わりうる

### 4.2 designated root
root を site policy で固定する。 `[S20][S22]`

向く場面:
- 入口を明確にしたい
- server 直結 node がある
- building gateway box を root にしたい

### 4.3 repo default
- **小規模 / 単純 site**: self-organized でもよい
- **本番 / 複数 domain / 複数 server**: designated root 推奨

理由:
- route stability
- observability
- operational predictability

---

## 5. Parent policy

### inputs
- RSSI / link quality
- parent current child count
- domain depth
- upstream health
- hold-down timers

### repo rules
- shallow is usually better
- unstable parent へはすぐ戻らない
- parent flaps を fabric health に出す
- powered control island は無理に deep chain にしない

---

## 6. Max layers guidance

Espressif API では max layer を設定でき、tree topology で最大 25、chain topology で最大 1000 とされています。 `[S21]`  
ただし、それは API 上の上限であり、実運用の推奨ではありません。

repo design guidance:
- v1 推奨 depth: **3〜5**
- 6 以上は site-specific tuning
- “遠すぎる powered island” は別 domain + bridge を検討
- control-heavy zone は shallow domain を優先

---

## 7. Root to Site Router bridge

root は upstream IP network へ出る入口になります。 `[S21]`  
この repo では root から Site Router への bridge を 3 パターン許可します。

### A. local IP
同一 LAN / VLAN / Wi-Fi upstream

### B. local host bridge
root に直結した host agent 経由

### C. special backhaul
4G / VPN / point-to-point Wi-Fi / future wifi_lr など

---

## 8. Multiple domains in one site

### allowed
- building A domain
- building B domain
- floor domain
- outdoor domain

### not assumed
- no-router で multiple roots が勝手に直接話すこと `[S22]`

### required
- Site Router / Fabric Spine / bridge で上位統合すること

---

## 9. Domain health signals

各 domain は少なくとも次を health として出す。

- root present
- root upstream reachable
- child count
- parent flaps / minute
- queue toDS backlog
- average depth
- degraded flag
- local-only flag

---

## 10. When not to use Wi-Fi mesh as primary

- deep sleep battery nodes
- ultra sparse event-only edge
- isolated long-range point where Wi-Fi attach costが高い
- JP-safe long-distance summary link where LoRa is more appropriate

このときは LoRa edge + bridge へ寄せる。
