# Firmware Components

最初に全 runtime を並べず、再利用可能な component から切ります。

- `fabric_proto`
  compact envelope / summary codec IDs
- `radio_hal_sx1262`
  JP-safe profile 前提の radio HAL
- `usb_link`
  gateway_head 用 USB CDC framing
- `node_outbox`
  local queue / retry
- `wifi_direct`
  powered leaf direct path
- `board_hal`
  XIAO ESP32S3 + Wio-SX1262 board helpers
