# Support Matrix

公開前の現時点で、どこまでを「試せる」「制限付き」「未対応」とみなすかをまとめます。

## Runtime / Platform

| Target | Runtime | 必須ツール | 現状 | Notes |
| --- | --- | --- | --- | --- |
| Ubuntu Server | Go mainline | `Go 1.25.x` | 開発本線 | `Site Router` / `Host Agent` / SDK / durable core の主対象 |
| Windows 開発環境 | Go mainline | `Go 1.25.x`, `python` | 制限付きサポート | 開発・検証向け。公開物と CI の正本は `go` / `python` コマンド |
| Python reference | Python 3.12+ | `python`, `pip` | reference-only | 契約比較、fixture、behavior comparison 用 |
| ESP32-S3 gateway-head | ESP-IDF 5.2+ | `idf.py`, 実機ボード | prototype | binary on-air frame を USB/LoRa 間で中継する。CI smoke compile はあるが real backend の HIL は未確認 |
| ESP32-S3 sleepy leaf | ESP-IDF 5.2+ | `idf.py`, 実機ボード | prototype | binary on-air の state/event/pending-digest/tiny-poll/compact-command/result を使う。heartbeat uplink と app loop 統合はまだ限定的。CI smoke compile はあるが deep sleep / HIL は未完了 |

## Feature Status

| Feature | Status | 検証 | Notes |
| --- | --- | --- | --- |
| Site Router durable core | active | Go test 主線 / Python comparison | SQLite ledger, dedupe, queue, command lifecycle, manifest/lease storage, role gate |
| Host Agent direct ingest | active | Go test / direct demos | JSON fixture, compact/summary relay, short-ID aware binary on-air state/event/heartbeat decode, gateway heartbeat durable ingest, spool diagnostics |
| Go app-facing SDK entrypoint | active | Go test | `pkg/sdk.OpenLocalSite()` に加え、`pkg/fabric` が state/event/sleepy command の typed entrypoint を提供する |
| Lease / role enforcement | limited | Go test | sleepy/battery node に always-on role を与えない gate、short ID lookup、lease bearer と manifest supported bearer の整合を実装 |
| Payload fit / enqueue gate | limited | Go test | `sleepy_tiny_control` は enqueue 前に compact fit を確認し、lease / short ID が無いと reject。route_class 未指定の rich payload は LoRa primary に暗黙投入しない |
| RoutePlan persistence | limited | Go test | selected bearer / route status / route reason / payload fit / route_plan_json を outbox に保存し、diagnostics / explain-route の土台にする |
| Command token correlation | limited | Go test | 16-bit token は compact token が必要な route だけに割り当て、target node scope で解決する。global unique 前提から外したが、lease epoch/window 化は今後 |
| Sleepy command acceptance flow | limited | `cmd/sleepy-cycle-demo` / development backend smoke | `issue -> digest -> poll -> command_result` を short-ID aware binary on-air demo と development backend smoke で確認 |
| Gateway runtime scaffold | prototype | コードレビュー / development backend | on-air header を優先して USB frame type を決める。`lora_ingress` heartbeat は `live=true` と subject metadata を出す。raw JSON over LoRa と legacy compact fallback は development backend 用に制限 |
| Node runtime scaffold | prototype | development backend smoke / ESP-IDF build | synthetic digest/command を binary on-air で 1 回たどる最小スモーク。fixed compact event uplink は実装済みだが、heartbeat body と sleepy app loop への統合は継続中 |
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
| `doctor.py` | 既定は layout / contract check。`--track go` / `--require-go` は `go` が存在しても実行不能なら失敗 |
| ESP-IDF `idf.py build` | CI smoke compile を構成済み。ローカル workspace 実行は環境依存 |
| 実機 HIL | 未実行 |
| soak / perf / security gate | 未完了 |
