# ESP-IDF Apps

このディレクトリは root repo 側の ESP-IDF 実装です。

- `gateway-head`
  USB CDC first の radio head
- `node-sdk`
  sleepy leaf sample
- `components`
  共通 component

現時点では scaffold と protocol/core component を提供し、`idf.py build` 前提の構造を先に固定しています。
この workspace に `idf.py` がない場合は、まず root の demo / doctor / support matrix を確認してください。

推奨バージョン:

- `ESP-IDF 5.2+`
- `target: esp32s3`

最初のビルド:

[要: ESP-IDF環境]
```bash
cd firmware/esp-idf/gateway-head
idf.py set-target esp32s3
idf.py build
idf.py -p <PORT> flash monitor
```

[要: ESP-IDF環境]
```bash
cd firmware/esp-idf/node-sdk
idf.py set-target esp32s3
idf.py build
idf.py -p <PORT> flash monitor
```

runtime の中心:

- `gateway-head/main/gateway_head_runtime.c`
- `node-sdk/main/sleepy_policy.c`

期待ログの最小目安:

- `gateway-head`
  `gateway runtime task started`
- `node-sdk`
  `sleepy cycle: uplink -> rx window -> sleep`
