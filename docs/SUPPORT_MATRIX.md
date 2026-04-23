# Support Matrix

公開前の現時点で、どこまでを「試せる」「制限付き」「未対応」とみなすかをまとめます。

## Runtime / Platform

| Target | Runtime | 必須ツール | 現状 | Notes |
| --- | --- | --- | --- | --- |
| Ubuntu Server | Go mainline | `Go 1.26.x` | 開発本線 | `Site Router` / `Host Agent` / SDK / durable core の主対象 |
| Windows 開発環境 | Go bundled toolchain | `.\.tools\go-sdk\bin\go.exe` | 制限付きサポート | 開発・検証向け。production deployment の主対象ではない |
| Python reference | Python 3.12+ | `python`, `pip` | reference-only | 契約比較、fixture、behavior comparison 用 |
| ESP32-S3 gateway-head | ESP-IDF 5.2+ | `idf.py`, 実機ボード | prototype | backend seam と development backend はあるが、実機 USB CDC / SX1262 backend は未完成 |
| ESP32-S3 sleepy leaf | ESP-IDF 5.2+ | `idf.py`, 実機ボード | prototype | bounded RX window と tiny poll 契約はあるが、実 downlink driver / HIL は未完了 |

## Feature Status

| Feature | Status | 検証 | Notes |
| --- | --- | --- | --- |
| Site Router durable core | active | Go test / Python reference | SQLite ledger, dedupe, queue, command lifecycle |
| Host Agent direct ingest | active | Go test / direct demos | JSON fixture, compact/summary relay, spool diagnostics |
| Sleepy command acceptance flow | active | `cmd/sleepy-cycle-demo` | `issue -> digest -> poll -> command_result` を確認済み |
| Gateway runtime scaffold | prototype | コードレビュー / development backend | explicit backend install 前提。default backend は dev only |
| Node runtime scaffold | prototype | development backend smoke | synthetic digest/command を 1 回たどる最小スモーク |
| Real USB CDC backend | planned | 未検証 | injectable seam の先の本実装が未完了 |
| Real SX1262 backend | planned | 未検証 | injectable seam の先の本実装が未完了 |
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
