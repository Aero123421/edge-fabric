# Support Matrix

公開前の現時点で、どこまでを「試せる」「制限付き」「未対応」とみなすかをまとめます。

## Runtime / Platform

| Target | Runtime | 必須ツール | 現状 | Notes |
| --- | --- | --- | --- | --- |
| Ubuntu Server | Go mainline | `Go 1.25.x` | 開発本線 | `Site Router` / `Host Agent` / SDK / durable core の主対象 |
| Windows 開発環境 | Go mainline | `Go 1.25.x`, `python` | 制限付きサポート | 開発・検証向け。公開物と CI の正本は `go` / `python` コマンド |
| Python reference | Python 3.12+ | `python`, `pip` | reference-only | 契約比較、fixture、behavior comparison 用 |
| ESP32-S3 gateway-head | ESP-IDF 5.2+ | `idf.py`, 実機ボード | prototype | binary on-air frame を USB/LoRa 間で中継する。CI smoke compile はあるが real backend の HIL は未確認 |
| ESP32-S3 sleepy leaf | ESP-IDF 5.2+ | `idf.py`, 実機ボード | prototype | binary on-air の state/pending-digest/tiny-poll/compact-command/result を使う。CI smoke compile はあるが deep sleep / HIL は未完了 |

## Feature Status

| Feature | Status | 検証 | Notes |
| --- | --- | --- | --- |
| Site Router durable core | active | Go test 主線 / Python comparison | SQLite ledger, dedupe, queue, command lifecycle, manifest/lease storage, role gate |
| Host Agent direct ingest | active | Go test / direct demos | JSON fixture, compact/summary relay, short-ID aware binary on-air decode, gateway heartbeat durable ingest, spool diagnostics |
| Lease / role enforcement | limited | Go test | sleepy/battery node に always-on role を与えない gate と short ID lookup を実装 |
| Payload fit / enqueue gate | limited | Go test | `sleepy_tiny_control` は enqueue 前に compact fit を確認し、lease / short ID が無いと reject |
| Command token correlation | limited | Go test | 16-bit token は target node scope で解決し、global unique 前提から外した。lease epoch/window 化は今後 |
| Sleepy command acceptance flow | limited | `cmd/sleepy-cycle-demo` / development backend smoke | `issue -> digest -> poll -> command_result` を short-ID aware binary on-air demo と development backend smoke で確認 |
| Gateway runtime scaffold | prototype | コードレビュー / development backend | on-air header を優先して USB frame type を決める。legacy compact 互換のため最小フォールバックは残している |
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
| `doctor.py --require-go` | 実行中。`go` が存在しても実行不能なら失敗 |
| ESP-IDF `idf.py build` | CI smoke compile を構成済み。ローカル workspace 実行は環境依存 |
| 実機 HIL | 未実行 |
| soak / perf / security gate | 未完了 |
