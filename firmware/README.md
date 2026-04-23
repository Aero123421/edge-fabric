# Firmware

firmware 側は ESP-IDF first で進めます。

推奨バージョンは `ESP-IDF 5.2+` です。

現時点の本線レイアウト:

- `firmware/esp-idf/gateway-head`
  USB CDC first の gateway head app
- `firmware/esp-idf/node-sdk`
  sleepy leaf sample app
- `firmware/esp-idf/components`
  `fabric_proto`, `usb_link`, `radio_hal_sx1262`, `board_xiao_sx1262`

初期 component plan:

- `fabric_proto`
- `radio_hal_sx1262`
- `usb_link`
- `node_outbox`
- `wifi_direct`
- `board_hal`

ビルドの入口:

- `firmware/esp-idf/gateway-head`
- `firmware/esp-idf/node-sdk`

各 app で:

[要: ESP-IDF環境]
```bash
idf.py set-target esp32s3
idf.py build
idf.py -p <PORT> flash monitor
```

この段階では compileable scaffold から始め、以後 `idf.py build` を通す方向で広げます。

XIAO ESP32-S3 + SX1262 を想定する最短確認:

- `gateway-head`
  USB 接続した XIAO を flash し、monitor で `gateway runtime task started` が見えること
- `node-sdk`
  sleepy leaf sample を flash し、monitor で `sleepy cycle: uplink -> rx window -> sleep` が見えること

現在の最小実装:

- `gateway-head`
  USB frame ingest / LoRa TX / LoRa RX relay / heartbeat
- `node-sdk`
  sleepy uplink / bounded RX window / pending digest / tiny poll / tiny command result

backend の現状:

- scaffold app は explicit に default development backend を install する
- `usb_link_install_backend()` / `radio_hal_install_backend()` に実機 driver を差し替えられる
- この repo の default backend は local handoff / poll seam の確認用で、実機 USB CDC / SX1262 完了通知はまだ未実装
- `node-sdk` は synthetic pending digest / tiny command を 1 回返す development backend を持つ
