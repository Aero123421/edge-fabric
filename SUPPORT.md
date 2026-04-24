# Support

この repo の本線は `Go + ESP-IDF`、`Python` は reference track です。

## Track Status

- `Go mainline`
  active
  `cmd/site-router`, `cmd/host-agent`, `internal/siterouter`, `internal/hostagent`
- `ESP-IDF firmware`
  prototype
  `gateway-head`, `node-sdk`, `fabric_proto`, `usb_link`, `radio_hal_sx1262`, `board_xiao_sx1262`
- `Python reference`
  reference-only
  contract validation と behavior comparison 用
- `Wi-Fi mesh / LoRa relay / hybrid routing`
  planned

## 現時点の制約

- `ESP-IDF firmware` は runtime と protocol の骨格はありますが、実機 USB CDC / 実 SX1262 backend は injectable API 段階です
- `gateway-head` の heartbeat `hop_buffered` は local handoff を意味し、actual radio TX completion や durable completion は意味しません
- `node-sdk` は sleepy leaf の bounded downlink / tiny command 契約を優先しており、rich command や OTA 本体は maintenance path 前提です
- `Wi-Fi mesh backbone`, `LoRa relay`, `hybrid routing`, `multi-domain` は GA ではなく段階実装中です

詳細な platform / feature ごとの状態は [docs/SUPPORT_MATRIX.md](docs/SUPPORT_MATRIX.md) を参照してください。

## Support Matrix Preview

- `Ubuntu Server + Go`
  開発中の本線
- `Windows + Go`
  標準 Go toolchain / devcontainer / CI と同じ clean source 前提の開発・検証向け
- `Python reference`
  契約比較/fixture 検証向け
- `ESP32-S3 + XIAO SX1262 board mapping`
  board support あり、実機 backend は未完成

## 連絡の目安

- 一般的な使い方や不具合相談
  GitHub Issue
- 脆弱性報告
  [SECURITY.md](SECURITY.md) を参照

現時点ではメンテナの応答 SLA は固定していません。公開前後で窓口が固まるまでは、Issue では再現条件を中心に共有してください。

仕様の根拠は [edge-fabric-esp32sx1262-v3-mesh](edge-fabric-esp32sx1262-v3-mesh/README.md) を参照してください。
