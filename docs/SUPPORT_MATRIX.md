# Support Matrix

公開前の現時点で、どこまでを「試せる」「制限付き」「未対応」とみなすかをまとめます。

## Runtime / Platform

| Target | Runtime | 必須ツール | 現状 | Notes |
| --- | --- | --- | --- | --- |
| Ubuntu Server | Go mainline | `Go 1.26.x` | 開発本線 | `Site Router` / `Host Agent` / SDK / durable core の主対象 |
| Windows 開発環境 | Go bundled toolchain | `.\.tools\go-sdk\bin\go.exe` | 制限付きサポート | 開発・検証向け。production deployment の主対象ではない |
| Python reference | Python 3.12+ | `python`, `pip` | reference-only | 契約比較、fixture、behavior comparison 用 |
| ESP32-S3 gateway-head | ESP-IDF 5.2+ | `idf.py`, 実機ボード | prototype | binary on-air frame を USB/LoRa 間で中継する。TinyUSB / SX1262 real backend は prototype、compile/HIL は未確認 |
| ESP32-S3 sleepy leaf | ESP-IDF 5.2+ | `idf.py`, 実機ボード | prototype | binary on-air の state/pending-digest/tiny-poll/compact-command/result を使う。light sleep は入ったが deep sleep / HIL は未完了 |

## Feature Status

| Feature | Status | 検証 | Notes |
| --- | --- | --- | --- |
| Site Router durable core | active | Go test / Python reference | SQLite ledger, dedupe, queue, command lifecycle, manifest/lease storage, role gate |
| Host Agent direct ingest | active | Go test / direct demos | JSON fixture, compact/summary relay, short-ID aware binary on-air decode, spool diagnostics |
| Lease / role enforcement | limited | Go test | sleepy/battery node に always-on role を与えない gate と short ID lookup を実装 |
| Payload fit / enqueue gate | limited | Go test | `sleepy_tiny_control` は enqueue 前に compact fit を確認し、lease / short ID が無いと reject |
| Sleepy command acceptance flow | limited | `cmd/sleepy-cycle-demo` / development backend smoke | `issue -> digest -> poll -> command_result` を binary on-air で確認済み |
| Gateway runtime scaffold | prototype | コードレビュー / development backend | compact/summary の heuristic 推測をやめ、on-air header を見て USB frame type を決める |
| Node runtime scaffold | prototype | development backend smoke | synthetic digest/command を binary on-air で 1 回たどる最小スモーク |
| Real USB CDC backend | prototype | コード実装あり / HIL未検証 | TinyUSB CDC-ACM backend を追加済み |
| Real SX1262 backend | prototype | コード実装あり / HIL未検証 | official `sx126x_driver` vendor + HAL 実装を追加済み |
| Wi-Fi mesh backbone | planned | 未検証 | roadmap v6 以降 |
| LoRa 1-relay / 2-relay | planned | 未検証 | roadmap v7-v8 |
| Hybrid routing / multi-domain | planned | 未検証 | roadmap v9-v10 |
| OTA / maintenance transfer | limited | 契約のみ一部 | maintenance path 前提。runtime は未完成 |

## Release Gate の現状

| Gate | 現状 |
| --- | --- |
| Go unit / integration | 実行中 |
| Python reference / artifact checks | 実行中 |
| ESP-IDF `idf.py build` | この workspace では未実行 |
| 実機 HIL | 未実行 |
| soak / perf / security gate | 未完了 |
