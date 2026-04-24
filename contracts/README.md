# Contracts

このディレクトリは root 実装側の contract / protocol freeze artifact です。

## 目的

- logic schema と wire protocol を分けて固定する
- fixture を回して後方互換を監視する
- JP-safe profile を初期条件として保持する

## いま入っているもの

- `fixtures/`
  envelope / manifest / lease の fixture
- `protocol/ack-phases.json`
  command / persist ack の phase 定義
- `protocol/jp-safe-profiles.json`
  JP-safe profile table
- `protocol/usb-cdc-frame.json`
  gateway_head 向け USB CDC framing 定義
- `protocol/onair-v1.json`
  Go mainline / ESP-IDF firmware が共有する binary on-air header と token 定義
- `protocol/compact-codecs.json`
  legacy compact/reference track の frame type と logical shape の固定表
  ここでの frame type `3/4` は USB transport 上の compact/summary family を指し、LoRa on-air header 自体の正本ではありません
- `protocol/heartbeat-wire.json`
  gateway heartbeat JSON と legacy compact heartbeat shape の固定表
- `protocol/sleepy-command-policy.json`
  sleepy leaf が通常 wake で受ける command policy
- `policy/role-policy.json`
  role ごとの relay / deep sleep / always-on 制約
- `policy/route-classes.json`
  route_class ごとの許可 bearer、payload 方針、service level
- `policy/device-profiles.json`
  motion / leak / powered servo など、汎用 IoT 用途の安全な初期 profile

## 利用トラック

- `Go mainline`
  wire / contract validation、binary on-air decode、acceptance fixture
- `ESP-IDF firmware`
  USB frame type、binary on-air、JP-safe profile、sleepy command policy の実装指針
- `Python reference`
  legacy compact artifact sync と regression 比較

## 契約を変える順番

1. fixture / example を更新する
2. `contracts/protocol/*.json` を更新する
3. Go / Python のテストと doctor を更新する
4. firmware README / limitations / support matrix の差分を確認する

## 正本の扱い

- binary on-air の正本は `contracts/protocol/onair-v1.json`
- `compact-codecs.json` は Python reference / legacy compact 比較用 artifact
- legacy compact は互換比較・shape 回帰のために残しており、新規 mainline 送信の正本ではありません
- `heartbeat-wire.json` の LoRa shape も legacy compact/reference track 用で、mainline binary on-air の heartbeat body とは分けて扱います
- `heartbeat-wire.json` の gateway JSON shape は `status_heartbeat_v1` と `lora_ingress_v1` を併記し、gateway runtime の 2 系統を legacy/reference track として管理します
- `sleepy-command-policy.json` は sleepy leaf command subset の正本
- `policy/*.json` は runtime 実装を増やす前に docs / SDK / tests が共有する safety policy の正本として扱います
