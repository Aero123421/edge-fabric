# Third-Party Sources

このディレクトリには、`radio_hal_sx1262` の実機 backend で利用する third-party source を置きます。

- `sx126x_driver/`
  - upstream: `https://github.com/Lora-net/sx126x_driver`
  - license: `BSD-3-Clause-Clear`
  - local use: `sx126x.c`, `sx126x.h`, `sx126x_hal.h`, `sx126x_regs.h`, `sx126x_status.h`

HAL 実装本体は `src/radio_hal_real_sx1262.c` 側で持ち、この vendor code 自体は直接改変しない方針です。
