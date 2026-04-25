# edge-fabric

`edge-fabric` は、ESP32-S3 + SX1262 デバイスを LoRa と Wi-Fi の両方で扱うための **hybrid IoT fabric SDK / framework** です。

アプリ開発者は transport を直接選ばず、`state`、`event`、`command`、`heartbeat`、`device profile` を発行します。Site Router が durable ledger / queue / route policy を管理し、Host Agent と ESP-IDF firmware が USB gateway、LoRa on-air frame、sleepy node の実行パスをつなぎます。

> Status: **strong alpha / pre-beta**<br>
> Core protocol、Site Router、Host Agent、`pkg/fabric` SDK、policy-driven RoutePlanner、sleepy tiny command、gateway/node heartbeat は実装済みです。実機 HIL、production key model、完全な Wi-Fi mesh / LoRa multi-hop data plane はまだ release gate 前です。

## What It Does

| Area | What you get |
| --- | --- |
| App SDK | `pkg/fabric` で state / event / sleepy command / device registration を扱う typed API |
| Durable core | SQLite-backed Site Router, message ledger, event/state projection, command ledger, outbox queue |
| Routing policy | DeviceProfile, role policy, route classes, RadioBudget, strict heartbeat subject, route diagnostics |
| LoRa protocol | [binary on-air v1](contracts/protocol/onair-v1.json), compact event/state/heartbeat/command/result, short ID, JP payload cap |
| Gateway path | Host Agent + ESP-IDF gateway-head for USB CDC / LoRa handoff and durable gateway heartbeat |
| Sleepy nodes | ESP-IDF node-sdk for compact event uplink, tiny command polling, opt-in deep sleep + RTC persistence |
| Diagnostics | `edge-fabric doctor`, `explain-route`, `queue-metrics`, `decode-onair`, `decode-usb-frame`, clean source export |

## What You Can Build

- Battery motion / leak sensors that wake, emit compact LoRa events, poll for tiny commands, then sleep.
- Powered Wi-Fi controllers that publish state and receive local commands without putting rich control traffic on LoRa.
- USB LoRa gateways that normalize radio observations and heartbeat into a durable Site Router.
- Hybrid route experiments where route classes enforce “LoRa only if compact and within budget” or “Wi-Fi only for bulk/control”.
- Mesh / relay prototypes using `relay_extension_v1`, `lora_relay_1`, `wifi_mesh_backbone`, and route diagnostics.

## What It Is Not

- It is **not LoRaWAN** and not a LoRaWAN concentrator replacement.
- It is **not production-certified security** yet. Runtime gates exist, but real key provisioning / signed lease / anti-replay are still hardening work.
- It is **not fully HIL-validated firmware** yet. ESP-IDF apps and strict backend guards exist, but board-level deep sleep, RF switch, USB DTR/backpressure, and multi-hop behavior still need hardware validation.

## Quick Look

```go
client, err := fabric.OpenLocal("site.db", "controller-01")
if err != nil {
    return err
}
defer client.Close()

_, err = client.PublishState(ctx, fabric.State{
    Source: "sensor-01",
    Key:    "temperature.c",
    Value:  24.5,
})

_, err = client.EmitEvent(ctx, fabric.Event{
    Source:   "motion-01",
    Type:     fabric.EventMotionDetected,
    Severity: fabric.Critical,
})
```

```bash
go run ./cmd/edge-fabric explain-route \
  -seed-fixtures \
  -fixture ./contracts/fixtures/command-sleepy-threshold-set.json
```

## Architecture

```text
Application / pkg/fabric
        |
        v
Site Router
  durable ledger / state projection / command queue / RoutePlanner
        |
        +--> Host Agent --> USB CDC gateway --> SX1262 LoRa
        |
        +--> Wi-Fi / local control path
        |
        +--> ESP-IDF sleepy leaf / gateway firmware contracts
```

## Repository Tracks

| Track | Purpose |
| --- | --- |
| Go mainline | Site Router, Host Agent, CLI, SDK, RoutePlanner, durable core |
| ESP-IDF firmware | `gateway-head`, `node-sdk`, board I/O, USB/radio backend, sleepy cycle |
| Contracts | Protocol artifacts, policy artifacts, fixtures, compatibility tests |
| Python reference | Contract validation and legacy/reference behavior comparison |

Python reference は binary on-air の正本ではなく、legacy compact regression と contract 検証のための non-authoritative reference track です。

The implementation source is in `contracts/`, `internal/`, `pkg/`, `cmd/`, `firmware/`, `src/`, and `tests/`. `edge-fabric-esp32sx1262-v3-mesh/` is the reference/ADR material, not the main runtime implementation.

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

### 1. Run the main checks

PowerShell:

```powershell
go test ./...
go run .\cmd\edge-fabric doctor
go run .\cmd\sleepy-cycle-demo
```

Bash:

```bash
go test ./...
go run ./cmd/edge-fabric doctor
go run ./cmd/sleepy-cycle-demo
```

### 2. Try the app-facing SDK

```go
package main

import (
	"context"

	"github.com/Aero123421/edge-fabric/pkg/fabric"
)

func main() error {
	ctx := context.Background()
	client, err := fabric.OpenLocal("site.db", "controller-01")
	if err != nil {
		return err
	}
	defer client.Close()

	_, err = client.PublishState(ctx, fabric.State{
		Source: "sensor-01",
		Key:    "temperature.c",
		Value:  24.5,
	})
	return err
}
```

外向き SDK は `pkg/fabric` を優先します。`pkg/sdk` は lower-level integration layer で、`internal/siterouter` は実装詳細です。

### 3. Use the diagnostics CLI

| Command | Purpose |
| --- | --- |
| `edge-fabric seed-fixtures` | local demo 用 manifest / lease を投入 |
| `edge-fabric issue-command -seed-fixtures -fixture ...` | sleepy command を durable queue に発行 |
| `edge-fabric explain-route -seed-fixtures -fixture ...` | なぜその route になるかを説明 |
| `edge-fabric queue-metrics` | ready / pending / blocked queue を確認 |
| `edge-fabric describe-profile -profile motion_sensor_battery_v1` | DeviceProfile の安全制約を表示 |
| `edge-fabric decode-onair -hex <hex-frame>` | LoRa on-air binary frame を decode |
| `edge-fabric decode-usb-frame -hex <hex-frame>` | USB CDC frame を decode |

Example:

```bash
go run ./cmd/edge-fabric explain-route \
  -seed-fixtures \
  -fixture ./contracts/fixtures/command-sleepy-threshold-set.json
```

### 4. Run examples

```bash
go run ./examples/01-basic-state
go run ./examples/02-critical-event
```

These examples use temporary local SQLite databases and do not require hardware.

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
