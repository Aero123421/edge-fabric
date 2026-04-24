# edge-fabric

この repo は、`ESP32-S3 + SX1262` を前提にした
LoRa + Wi-Fi ハイブリッド fabric の **実装リポジトリ** です。

現状は **strong alpha / pre-beta** です。`durable Site Router`、`Host Agent`、
binary on-air v1、sleepy tiny command、gateway heartbeat ingest は動く本線として整備していますが、
Wi-Fi mesh backbone、LoRa relay / multi-hop、自動 hybrid route selection、本番 provisioning、
deep sleep field deployment はまだ完成扱いではありません。

この project は raw LoRa-style `SX1262` link と custom fabric protocol を使います。
**LoRaWAN stack ではなく、LoRaWAN concentrator の代替でもありません。**

このルート repo では、次の 2 つを明確に分けます。

- `edge-fabric-esp32sx1262-v3-mesh/`
  仕様・ADR・計算資料の参照元
- ルート直下の `contracts/`, `src/`, `host/`, `sdk/`, `firmware/`, `tests/`
  実装本体

## 現在の実装方針

Current Alpha Can:

- JSON event/state/command を Site Router の durable ledger / queue に ingest する
- USB gateway heartbeat を Host Agent で `heartbeat` envelope に正規化し、durable ledger に保存する
- binary on-air v1 の `state / event / command_result / pending_digest / tiny_poll / compact_command / heartbeat` を encode/decode する
- sleepy tiny command を short ID / command token / JP payload cap 前提で扱う
- clean source export を生成し、Python / Go / firmware build smoke CI で主線を守る

Current Alpha Cannot Yet:

- Wi-Fi mesh backbone / LoRa relay / multi-hop routing を本番品質で運用する
- app intent から bearer/path を完全自動選択する汎用 RoutePlanner として動く
- battery sleepy leaf を deep sleep + RTC persistence の field deployment として保証する
- production security key model / signed lease / anti-replay を提供する

- `Site Router` を durable single writer とする
- app-facing API に bearer 名を出さない
- `gateway_head` は USB CDC first
- LoRa は `JP-safe profile` を初期条件として扱う
- gateway heartbeat は Host Agent で `heartbeat` envelope に正規化し、Site Router の durable ledger に入れる
- binary on-air の正本は `contracts/protocol/onair-v1.json` に置く
- `contracts/protocol/compact-codecs.json` は legacy compact/reference track の artifact として扱う
- `compact-codecs.json` の frame type `3/4` は USB transport family の shape 管理で、LoRa on-air header そのものの正本ではない
- LoRa on-air は short ID 前提の binary header / compact command token を優先する
- `manifest / lease / role / power-class` を queue 前に反映する
- `contract -> integration -> HIL -> soak` を各フェーズで gate にする

## 実装トラック

- `Go mainline`
  Ubuntu Server 上で動く `Site Router` / `Host Agent` の本線実装
- `ESP-IDF firmware`
  `gateway_head` / `sleepy_leaf` / board component の本線実装
- `Python reference`
  legacy compact regression と contract 検証のための参照実装。binary on-air の正本ではなく、GA 判定の主軸でもありません

本線は **`Go + ESP-IDF`** です。`Python` はあくまで参照実装で、挙動比較と fixture 検証に使います。

## 責務分担

| Track | 主責務 |
| --- | --- |
| `Go mainline` | durable state, queue, dedupe, `Site Router`, `Host Agent`, CLI / SDK |
| `ESP-IDF firmware` | board I/O, radio / USB backend, sleepy wake cycle, gateway runtime |
| `Python reference` | contract validation, fixture replay, behavior comparison |

## Requirements

- `Go 1.25.x`
  Ubuntu Server 側の mainline 実装
- `Python 3.12+`
  参照実装と補助スクリプト
- `ESP-IDF 5.2+`
  `ESP32-S3` firmware

`Go` は `PATH` 上の標準ツールチェーンを前提にします。maintainer ローカルの補助 toolchain を使う場合でも、公開物と CI の正本は `go` コマンドです。

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
go test ./...
python .\scripts\doctor.py --require-go
go run .\cmd\site-router -op doctor
go run .\cmd\site-router -op issue-command -fixture .\contracts\fixtures\command-sleepy-threshold-set.json
go run .\cmd\site-router -op pending-digest -hardware-id <leaf-hardware-id>
go run .\cmd\host-agent -mode direct-json -input .\contracts\fixtures\event-battery-alert.json
go run .\cmd\host-agent -mode diagnostics
go run .\cmd\direct-slice-demo
go run .\cmd\sleepy-cycle-demo
```

Bash:

```bash
go test ./...
python ./scripts/doctor.py --require-go
go run ./cmd/site-router -op doctor
go run ./cmd/direct-slice-demo
go run ./cmd/sleepy-cycle-demo
```

App-facing Go entrypoint:

```go
client, err := sdk.OpenLocalSite("site.db", "controller-01")
if err != nil {
    return err
}
defer client.Close()

_, err = client.PublishState(ctx, "sensor-01", "temperature.c", map[string]any{"value": 24.5}, "")
```

`pkg/sdk` の mainline entrypoint は `OpenLocalSite()` / public `ClientBackend` です。`internal/siterouter` は実装詳細なので、外部アプリは直接 import しなくても local durable router を使えます。

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
python .\scripts\export_clean_repo.py
```

`doctor.py` の既定モードは layout / contract の健全性を見ます。Go / firmware toolchain を必須化する場合は `--track go` / `--track firmware` / `--track all` または `--require-go` / `--require-idf` を使います。

`export_clean_repo.py` は git checkout では clean worktree を要求し、`HEAD` の source zip を作ります。生成済み source archive から実行した場合は、forbidden artifact を除外する filesystem fallback で zip を作ります。`--allow-dirty` は maintainer 向けのローカル確認用で、git checkout では未コミット変更を zip に含めません。

### ESP-IDF firmware

`ESP-IDF` が未導入でも、この repo では先に次を確認できます。

- [docs/SUPPORT_MATRIX.md](docs/SUPPORT_MATRIX.md)
- [docs/KNOWN_LIMITATIONS.md](docs/KNOWN_LIMITATIONS.md)
- Go / Python の demo と doctor

この workspace で `ESP-IDF` が未導入な場合、まだできないこと:

- 実機 flash / monitor
- TinyUSB / SX1262 real backend のローカル build 確認
- 実機 HIL

`ESP-IDF` が入っている環境では、各 app ディレクトリで次を実行します。

[要: ESP-IDF環境]

```bash
python ./scripts/doctor.py --require-go --require-idf
idf.py set-target esp32s3
idf.py build
```

対象 app:

- `firmware/esp-idf/gateway-head`
- `firmware/esp-idf/node-sdk`

この workspace では `idf.py` / `IDF_PATH` が未導入な場合があります。
その場合、repo 側では contract / demo / doctor / known limitations を先に維持し、実機 build は ESP-IDF 環境で追います。CI では `gateway-head` と `node-sdk` の `idf.py build` smoke を別 job で回します。

firmware 側の default identity は board MAC 由来で、`gw-XXXXXX` / `leaf-XXXXXX` と short ID を暫定生成します。
正式な site lease / provisioning 上書きは今後の実装対象です。

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
- durable gateway heartbeat
- target-scoped sleepy command token

`contracts/fixtures/command-servo-set-angle.json` は Site Router / command ledger の汎用デモ用です。
sleepy leaf と firmware `node-sdk` の確認には `command-sleepy-threshold-set.json` を使います。sleepy fixture は `battery-leaf-01` の manifest/lease 例に合わせています。

`Wi-Fi mesh backbone`, `LoRa 1/2-relay`, `hybrid route engine`,
`multi-domain / multi-host` は段階的に追加します。

詳しい support レベルは [SUPPORT.md](SUPPORT.md) を参照してください。
実行対象ごとの matrix は [docs/SUPPORT_MATRIX.md](docs/SUPPORT_MATRIX.md) を参照してください。
既知の制約は [docs/KNOWN_LIMITATIONS.md](docs/KNOWN_LIMITATIONS.md) を参照してください。
