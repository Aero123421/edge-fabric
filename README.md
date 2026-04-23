# edge-fabric

この repo は、`ESP32-S3 + SX1262` を前提にした
LoRa + Wi-Fi ハイブリッド fabric の **実装リポジトリ** です。

このルート repo では、次の 2 つを明確に分けます。

- `edge-fabric-esp32sx1262-v3-mesh/`
  仕様・ADR・計算資料の参照元
- ルート直下の `contracts/`, `src/`, `host/`, `sdk/`, `firmware/`, `tests/`
  実装本体

## 現在の実装方針

- `Site Router` を durable single writer とする
- app-facing API に bearer 名を出さない
- `gateway_head` は USB CDC first
- LoRa は `JP-safe profile` を初期条件として扱う
- compact / summary codec は `contracts/protocol/compact-codecs.json` を source of truth にする
- LoRa on-air は short ID 前提の binary header / compact command token を優先する
- `manifest / lease / role / power-class` を queue 前に反映する
- `contract -> integration -> HIL -> soak` を各フェーズで gate にする

## 実装トラック

- `Go mainline`
  Ubuntu Server 上で動く `Site Router` / `Host Agent` の本線実装
- `ESP-IDF firmware`
  `gateway_head` / `sleepy_leaf` / board component の本線実装
- `Python reference`
  contract 検証と挙動比較のための参照実装。GA 判定の主軸ではありません

本線は **`Go + ESP-IDF`** です。`Python` はあくまで参照実装で、挙動比較と fixture 検証に使います。

## 責務分担

| Track | 主責務 |
| --- | --- |
| `Go mainline` | durable state, queue, dedupe, `Site Router`, `Host Agent`, CLI / SDK |
| `ESP-IDF firmware` | board I/O, radio / USB backend, sleepy wake cycle, gateway runtime |
| `Python reference` | contract validation, fixture replay, behavior comparison |

## Requirements

- `Go 1.26.x`
  Ubuntu Server 側の mainline 実装
- `Python 3.12+`
  参照実装と補助スクリプト
- `ESP-IDF 5.2+`
  `ESP32-S3` firmware

ローカルに Go がない場合でも、この repo では Windows 向けに `.\.tools\go-sdk\bin\go.exe` を使えます。

## ディレクトリ

- [contracts](contracts/README.md)
  protocol freeze artifacts と fixtures
- `internal/`, `pkg/`, `cmd/`
  Go server 実装
- `src/`
  Python 参照実装
- [host](host/README.md)
  host 側実装メモ
- [sdk](sdk/README.md)
  SDK 方針
- [firmware](firmware/README.md)
  firmware 実装方針
- `tests/`
  contract / site-router テスト
- [docs/IMPLEMENTATION_ROADMAP.md](docs/IMPLEMENTATION_ROADMAP.md)
  v1-v11 の実装ロードマップ

## Quickstart

### Go mainline

PowerShell:

```powershell
.\.tools\go-sdk\bin\go.exe test ./...
.\.tools\go-sdk\bin\go.exe run .\cmd\site-router -op doctor
.\.tools\go-sdk\bin\go.exe run .\cmd\site-router -op issue-command -fixture .\contracts\fixtures\command-sleepy-threshold-set.json
.\.tools\go-sdk\bin\go.exe run .\cmd\site-router -op pending-digest -hardware-id sleepy-leaf-01
.\.tools\go-sdk\bin\go.exe run .\cmd\host-agent -mode direct-json -input .\contracts\fixtures\event-battery-alert.json
.\.tools\go-sdk\bin\go.exe run .\cmd\host-agent -mode diagnostics
.\.tools\go-sdk\bin\go.exe run .\cmd\direct-slice-demo
.\.tools\go-sdk\bin\go.exe run .\cmd\sleepy-cycle-demo
```

Bash:

```bash
go test ./...
go run ./cmd/direct-slice-demo
```

### Python reference

PowerShell:

```powershell
python -m venv .venv
.\.venv\Scripts\Activate.ps1
python -m pip install --upgrade pip
python -m pip install -e .
python .\scripts\doctor.py
python -m unittest discover -s tests -v
python .\scripts\demo_local_router.py
python .\scripts\simulate_direct_slice.py
```

### ESP-IDF firmware

`ESP-IDF` が未導入でも、この repo では先に次を確認できます。

- [docs/SUPPORT_MATRIX.md](docs/SUPPORT_MATRIX.md)
- [docs/KNOWN_LIMITATIONS.md](docs/KNOWN_LIMITATIONS.md)
- Go / Python の demo と doctor

この workspace で `ESP-IDF` が未導入な場合、まだできないこと:

- `idf.py build`
- 実機 flash / monitor
- TinyUSB / SX1262 real backend の compile 確認
- 実機 HIL

`ESP-IDF` が入っている環境では、各 app ディレクトリで次を実行します。

[要: ESP-IDF環境]

```bash
idf.py set-target esp32s3
idf.py build
```

対象 app:

- `firmware/esp-idf/gateway-head`
- `firmware/esp-idf/node-sdk`

この workspace では `idf.py` / `IDF_PATH` が未導入な場合があります。
その場合、repo 側では contract / demo / doctor / known limitations を先に維持し、実機 build は ESP-IDF 環境で追います。

## いま試せるもの

- `cmd/site-router`
  SQLite durable core を直接触る CLI
- `cmd/host-agent`
  JSON fixture や USB frame を relay し、spool diagnostics / flush も行える CLI
- `cmd/direct-slice-demo`
  Go mainline の `Host Agent -> Site Router` direct slice デモ
- `cmd/sleepy-cycle-demo`
  sleepy node の `issue -> digest -> poll -> command_result` acceptance デモ
- `scripts/demo_local_router.py`
  Python 参照実装の最小デモ
- `scripts/simulate_direct_slice.py`
  Python 参照実装の direct slice シミュレーション

## 現時点のスコープ

今の GA 対象はまだ固定していません。
ただし最初に強く仕上げる対象は次です。

- single site
- single logical writer
- direct LoRa uplink
- powered Wi-Fi direct
- bounded queue / dedupe / persist ack

`contracts/fixtures/command-servo-set-angle.json` は Site Router / command ledger の汎用デモ用です。
sleepy leaf と firmware `node-sdk` の確認には `command-sleepy-threshold-set.json` を使います。

`Wi-Fi mesh backbone`, `LoRa 1/2-relay`, `hybrid route engine`,
`multi-domain / multi-host` は段階的に追加します。

詳しい support レベルは [SUPPORT.md](SUPPORT.md) を参照してください。
実行対象ごとの matrix は [docs/SUPPORT_MATRIX.md](docs/SUPPORT_MATRIX.md) を参照してください。
既知の制約は [docs/KNOWN_LIMITATIONS.md](docs/KNOWN_LIMITATIONS.md) を参照してください。
