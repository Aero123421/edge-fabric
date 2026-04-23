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
- `protocol/compact-codecs.json`
  compact/summary frame type と logical shape の固定表
- `protocol/heartbeat-wire.json`
  gateway heartbeat JSON と LoRa heartbeat shape の固定表
- `protocol/sleepy-command-policy.json`
  sleepy leaf が通常 wake で受ける command policy

## 利用トラック

- `Go mainline`
  wire / contract validation、compact/summary decode、acceptance fixture
- `ESP-IDF firmware`
  USB frame type、JP-safe profile、sleepy command policy の実装指針
- `Python reference`
  artifact sync と regression 比較

## 契約を変える順番

1. fixture / example を更新する
2. `contracts/protocol/*.json` を更新する
3. Go / Python のテストと doctor を更新する
4. firmware README / limitations / support matrix の差分を確認する
