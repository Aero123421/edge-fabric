# Known Limitations

現時点で明示しておくべき制約です。

## Firmware

- `gateway-head` と `node-sdk` は backend seam を持ちますが、実機 USB CDC / 実 SX1262 driver はまだ開発中です
- app 起動時は TinyUSB が有効な build では real backend を優先しますが、prototype path の初期化に失敗した場合は **development backend** にフォールバックします
- `node-sdk` 側は synthetic pending digest / tiny command で RX path smoke を 1 回たどれ、scripted smoke API で分岐も再現できます
- `gateway-head` 側には TinyUSB CDC / SX1262 real backend の prototype がありますが、compile と HIL はまだ未確認です
- `hop_buffered` heartbeat は local enqueue / handoff を意味し、actual radio TX completion は意味しません
- `sleepy leaf` は sleepy-safe command を優先し、maintenance path の rich command / OTA はまだ限定的です
- `maintenance_awake` は bounded cycle で自動解除されますが、完全な power-state orchestration まではまだありません

## Routing

- `Wi-Fi mesh backbone`
- `LoRa 1-relay / 2-relay`
- `hybrid routing`
- `multi-domain / multi-host`

これらは実装中で、GA scope ではありません。

## Validation

- Go / Python の contract / integration / acceptance は増やしています
- ESP-IDF は `idf.py build` と実機 HIL をまだこの環境で回していません
- soak / perf / security gate は release hardening の残課題です
