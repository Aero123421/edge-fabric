# gateway-head

`gateway-head` は `USB CDC first` の gateway runtime です。

現時点の最小責務:

- USB frame を受けて LoRa TX へ渡す
- LoRa RX を USB envelope frame として返す
- startup / hop-buffered / ingress heartbeat を USB 側へ返す
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
- `main.c` は TinyUSB が有効な build では `gateway_head_runtime_use_real_backends()` を優先しますが、prototype path の初期化に失敗した場合は warning を出して `gateway_head_runtime_use_default_backends()` にフォールバックします
- 実機 backend に差し替える場合は
  `gateway_head_runtime_use_real_backends() -> gateway_head_runtime_start()`
  もしくは
  `gateway_head_runtime_init_transport() -> usb_tinyusb_backend_install() -> radio_hal_install_real_sx1262_backend() -> gateway_head_runtime_set_default_backends(false) -> gateway_head_runtime_start()`
  の順で呼びます
- scripted smoke をやり直すときは `gateway_head_backend_reset_smoke()` を呼びます

real backend の現状:

- USB 側は TinyUSB CDC-ACM を使う前提です
- radio 側は `Lora-net/sx126x_driver` を vendor し、HAL を `radio_hal_real_sx1262.c` で実装しています
- まだこの workspace では `idf.py build` / HIL を回していないため、real backend は **prototype** 扱いです

主な実装入口:

- `main/gateway_head_runtime.c`
- `components/usb_link`
- `components/radio_hal_sx1262`
- `components/fabric_proto`
