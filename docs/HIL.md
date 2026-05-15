# HIL smoke guide

この文書は ESP32-S3 + SX1262 gateway/head と sleepy leaf を実機で確認するための最小 HIL 手順です。
通常 CI や開発者 PC で実機がない場合、HIL は `EDGE_FABRIC_HIL=1` が設定されない限り skip して終了 0 になります。

## 対象

- `gateway-smoke`
  - gateway-head が TinyUSB CDC と SX1262 real backend で起動する。
  - TinyUSB DTR が heartbeat の `usb_dtr` に反映される。
  - DTR false 中の USB TX が backpressure として扱われ、`usb_tx_backpressure` が単調増加する。
  - Host から compact binary USB frame を投入すると gateway ack frame が返る。
  - SX1262 TX path へ compact command が handoff される。
- `gateway-node-smoke`
  - `gateway-smoke` に加えて sleepy leaf の log に `entering deep sleep`、`entering light sleep`、または `cycle complete` が現れる。
  - deep sleep build では timer wake 後に sequence/command-token cache が RTC persistence で継続することを別途 log と電流計で確認する。
- `gateway-relay-node`
  - relay firmware が入った時点で有効化する拡張 smoke。現状は relay log port が未設定なら skip 表示のみ。

## 実機構成

最小構成:

- 1 x Seeed XIAO ESP32-S3 + SX1262 gateway-head
- 1 x Seeed XIAO ESP32-S3 + SX1262 sleepy leaf
- gateway の USB CDC port
- sleepy leaf の serial log port
- deep sleep current 確認用の USB 電流計または電源アナライザ

gateway は `CONFIG_TINYUSB_CDC_ENABLED=y` かつ必要なら `CONFIG_EDGE_FABRIC_REQUIRE_REAL_BACKENDS=y` で build します。
sleepy leaf の deep sleep 確認では `CONFIG_EDGE_FABRIC_SLEEPY_USE_DEEP_SLEEP=y` と `CONFIG_EDGE_FABRIC_SLEEPY_ENABLE_RTC_PERSISTENCE=y` を有効にします。

## 事前 build / flash

ESP-IDF が入った shell で実行します。

```powershell
idf.py -C firmware/esp-idf/gateway-head build flash monitor
idf.py -C firmware/esp-idf/node-sdk build flash monitor
```

strict real backend smoke:

```powershell
idf.py -C firmware/esp-idf/gateway-head -D SDKCONFIG_DEFAULTS="sdkconfig.defaults;sdkconfig.production.defaults" build
```

`idf.py` がない環境では firmware build/HIL は実行できません。その場合は `scripts/hil_smoke.py self-test` だけを実行して host-side harness を検証します。

## HIL harness

host 側の実機 smoke は `scripts/hil_smoke.py` です。実機依存のため `EDGE_FABRIC_HIL=1` で明示的に有効化します。

自己テスト:

```powershell
python scripts/hil_smoke.py self-test
```

実機なし CI での skip 確認:

```powershell
python scripts/hil_smoke.py run --suite gateway-node-smoke
```
実機あり実行:

```powershell
$env:EDGE_FABRIC_HIL = "1"
$env:EDGE_FABRIC_HIL_GATEWAY_PORT = "COM7"
$env:EDGE_FABRIC_HIL_LEAF_LOG_PORT = "COM8"
python scripts/hil_smoke.py run --suite gateway-node-smoke
```

JSON config を使う場合:

```json
{
  "gateway_port": "COM7",
  "leaf_log_port": "COM8",
  "relay_log_port": null,
  "baud": 115200,
  "heartbeat_timeout_s": 15,
  "ack_timeout_s": 8,
  "dtr_backpressure_window_s": 6
}
```

```powershell
$env:EDGE_FABRIC_HIL = "1"
python scripts/hil_smoke.py run --suite gateway-node-smoke --config hil.local.json
```

## 期待される観測点

gateway boot log:

- `installed TinyUSB CDC backend`
- `sx1262 real backend`
- `gateway startup usb_backend=tinyusb-cdc-acm radio_backend=sx1262-real`

gateway heartbeat JSON:

- `subject_kind` が `gateway`
- `usb_dtr` が `true` または `false`
- `usb_tx_backpressure` が DTR false 区間後も単調増加
- `radio_tx_ok` / `radio_tx_fail` が SX1262 TX handoff 結果を反映

gateway ack:

- USB frame type `5`
- JSON payload の `status` が `gateway_accepted`、`radio_sent`、または `radio_failed`
- `source_frame_type` が投入した USB frame type

sleepy leaf:

- `sleepy cycle: uplink -> rx window -> sleep`
- `received pending digest`
- `sending explicit tiny poll`
- `sending command result`
- deep sleep build では `entering deep sleep`

## Deep sleep current check

自動 harness は serial log までを smoke とします。電流値は測定器依存が大きいため、現時点では手順チェックです。

1. sleepy leaf を `CONFIG_EDGE_FABRIC_SLEEPY_USE_DEEP_SLEEP=y` で flash する。
2. 電源アナライザを USB 5V または battery path に入れる。
3. `entering deep sleep` log の直後に平均電流が sleep profile まで落ちることを確認する。
4. timer wake 後に `cycle complete` が再度出ることを確認する。
5. compact command を 2 回送って、同じ command token の terminal result が重複送信されないことを確認する。

## CI policy

self-hosted runner では次のように HIL job を gated にします。

```yaml
env:
  EDGE_FABRIC_HIL: "1"
  EDGE_FABRIC_HIL_GATEWAY_PORT: "/dev/serial/by-id/..."
  EDGE_FABRIC_HIL_LEAF_LOG_PORT: "/dev/serial/by-id/..."

steps:
  - run: python scripts/hil_smoke.py self-test
  - run: python scripts/hil_smoke.py run --suite gateway-node-smoke
```

通常 runner では `EDGE_FABRIC_HIL` を設定せず、次の skip を許容します。

```powershell
python scripts/hil_smoke.py run --suite gateway-node-smoke
```
