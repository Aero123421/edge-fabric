# Known Limitations

現時点で明示しておくべき制約です。

## Firmware

- `gateway-head` と `node-sdk` は binary on-air frame を使い始めた。CI smoke compile は構成済みだが、実機 USB CDC / 実 SX1262 driver の HIL はまだ未確認です
- `node-sdk` 側は synthetic pending digest / tiny command で RX path smoke を 1 回たどれ、scripted smoke API で分岐も再現できます
- `gateway-head` 側には TinyUSB CDC / SX1262 real backend の prototype があります。CI smoke compile はあるが、実配線込みの HIL はまだ未確認です
- `hop_buffered` heartbeat は local enqueue / handoff を意味し、actual radio TX completion は意味しません
- `sleepy leaf` は sleepy-safe compact command subset を優先し、rich event codec / OTA / maintenance transfer はまだ限定的です
- `sleepy leaf` は light sleep まで入ったが、deep sleep / RTC wake / 完全な power-state orchestration まではまだありません
- firmware 側の default identity は board MAC 由来になったが、lease/provisioning での正式上書きはまだ未実装です
- binary on-air の正本は `contracts/protocol/onair-v1.json` に寄せていますが、`event` / `heartbeat` の body codec surface はまだ予約済みトークンの段階です

## Routing

- Go runtime では short ID / lease / payload-fit gate を先行実装し、firmware 側も binary on-air と short ID に追随し始めたが、deep sleep と path-aware queue はまだ未完了です
- `sleepy_tiny_control` の compact downlink は小さい command subset を優先し、rich payload / OTA / maintenance transfer はまだ summary / maintenance path 側です
- `Wi-Fi mesh backbone`
- `LoRa 1-relay / 2-relay`
- `hybrid routing`
- `multi-domain / multi-host`

これらは実装中で、GA scope ではありません。

## Validation

- Go / Python の contract / integration / acceptance は増やしています
- Python reference は legacy compact regression 用で、binary on-air の主線 validator ではありません
- ESP-IDF は CI で `idf.py build` smoke を回し始めたが、実機 HIL はまだこの環境で回していません
- soak / perf / security gate は release hardening の残課題です
