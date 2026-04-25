# gateway-head

`gateway-head` は `USB CDC first` の gateway runtime です。

現時点の最小責務:

- USB frame を受けて LoRa TX へ渡す
- LoRa RX を USB envelope frame として返す
- startup / hop-buffered / ingress heartbeat を USB 側へ返す
- heartbeat に USB/RF handoff counters と USB TX backpressure counter を載せる
- JP-safe LoRa profile を適用して起動する

現段階の `usb_link` / `radio_hal_sx1262` は injectable runtime backend です。
default backend は **development backend** で、production 用ではありません。

backend 差し込み面:

- `usb_link_install_backend(...)`
  実機 USB CDC の TX / RX polling を差し込む
- `radio_hal_install_backend(...)`
  実機 SX1262 profile apply / TX / RX polling を差し込む
- `usb_tinyusb_backend_install()`
  TinyUSB CDC-ACM を使う real USB backend を install する
- `radio_hal_install_real_sx1262_backend()`
  SX1262 SPI + DIO1/BUSY を使う real radio backend を install する
- `gateway_head_runtime_init_transport()`
  runtime が使う transport を初期化する
- `gateway_head_runtime_use_default_backends()`
  scaffold/dev backend を明示的に使う
- `gateway_head_runtime_use_real_backends()`
  TinyUSB + SX1262 real backend をまとめて install する
- `gateway_head_runtime_poll_once()`
  `usb_link_service()` / `radio_hal_service()` を回しつつ 1 step 進める
- `gateway_head_backend_script_usb_frame(...)`
  scripted USB ingress frame を 1 本注入する
- `gateway_head_backend_script_radio_frame(...)`
  scripted LoRa ingress frame を 1 本注入する
- `gateway_head_backend_get_last_usb_tx(...)` / `gateway_head_backend_get_last_radio_tx(...)`
  dev backend が最後に handoff した TX を観測する

起動条件:

- `gateway_head_runtime_start()` は delivery path が未設定だと `ESP_ERR_INVALID_STATE` を返します
- `main.c` は TinyUSB が有効な build では `gateway_head_runtime_use_real_backends()` を優先します。`CONFIG_EDGE_FABRIC_REQUIRE_REAL_BACKENDS=y` の build では prototype path の初期化に失敗した時点で fail-fast し、development backend へフォールバックしません。未設定時だけ warning を出して `gateway_head_runtime_use_default_backends()` にフォールバックします
- `CONFIG_EDGE_FABRIC_REQUIRE_REAL_BACKENDS=y` では `gateway_head_runtime_use_default_backends()` 自体も拒否し、`gateway_head_runtime_start()` が development backend 混入を検出して fail-fast します
- production hardening starter profile は `sdkconfig.production.defaults` を重ねて使います。この profile は real backend 必須にし、secure boot / flash encryption / NVS encryption の Kconfig を有効化するための出発点です。実機へ焼く前に ESP-IDF の production provisioning flow で secure boot / flash encryption key と eFuse の irreversible 設定を確認し、`idf.py -D SDKCONFIG_DEFAULTS=\"sdkconfig.defaults;sdkconfig.production.defaults\" reconfigure` 後の generated `sdkconfig` で期待する security option が有効になっていることを検証してください。development backend への silent fallback は禁止されます
- 実機 backend に差し替える場合は
  `gateway_head_runtime_use_real_backends() -> gateway_head_runtime_start()`
  もしくは
  `gateway_head_runtime_init_transport() -> usb_tinyusb_backend_install() -> radio_hal_install_real_sx1262_backend() -> gateway_head_runtime_set_default_backends(false) -> gateway_head_runtime_start()`
  の順で呼びます
- scripted smoke をやり直すときは `gateway_head_backend_reset_smoke()` を呼びます

real backend の現状:

- USB 側は TinyUSB CDC-ACM を使う前提です
- heartbeat の `usb_tx_ok` / `usb_tx_fail` / `usb_tx_backpressure` / `radio_tx_ok` / `radio_tx_fail` / `radio_rx_frames` / `usb_rx_frames` で handoff 状態を観測できます
- `usb_dtr` は development backend では `n/a`、TinyUSB real backend では現時点 `unknown` として明示します。DTR line-state 自体は TinyUSB backend log に出ますが、共通 `usb_link` API へ未公開のため HIL で追加確認が必要です
- radio 側は `Lora-net/sx126x_driver` を vendor し、HAL を `radio_hal_real_sx1262.c` で実装しています
- まだこの workspace では `idf.py build` / HIL を回していないため、real backend は **prototype** 扱いです

RF switch / HIL checklist:

- `BOARD_LORA_RF_SW1` は board init 時に High 固定で初期化する前提です。TX/RX polarity が module 実配線と合うことを HIL で確認してください
- SX1262 reset 後に BUSY が解除され、JP-safe profile apply 後に continuous RX へ戻ることを logic analyzer か GPIO trace で確認してください
- Host が USB CDC DTR を drop した状態で TX を詰め、`usb_tx_backpressure` が増えること、復帰後に heartbeat counters が更新されることを確認してください
- `CONFIG_EDGE_FABRIC_REQUIRE_REAL_BACKENDS=y` build で TinyUSB/SX1262 の片側を外し、development fallback せず fail-fast することを確認してください

主な実装入口:

- `main/gateway_head_runtime.c`
- `components/usb_link`
- `components/radio_hal_sx1262`
- `components/fabric_proto`
