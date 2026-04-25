# node-sdk

`node-sdk` は sleepy leaf の最小 runtime です。

現時点の最小責務:

- JP-safe LoRa profile を適用する
- wake cycle を繰り返す最小 loop を持つ
- binary on-air frame を使って short ID で uplink/downlink する
- uplink 後に bounded RX window を開く
- pending command digest を受ける
- tiny poll を返す
- tiny command を受けて command_result を返す
- maintenance_awake の受信窓を切り替える
- light sleep / opt-in deep sleep の cycle を Kconfig で切り替える
- RTC memory に sequence / short ID / maintenance countdown / recent command token cache を保持する

現段階の制約:

- compact downlink は binary on-air の bounded command kind を前提にした最小実装
- `maintenance_sync` や rich maintenance transfer はまだ未実装で、compact command path には入っていない
- board MAC 由来の default node ID / short ID を使う。lease/provisioning 上書きはまだ未実装
- compact state uplink は現時点では `node.power` の `awake/sleep` に絞っている
- compact event uplink は fixed body codec の範囲に絞っている
- scripted backend の compact command は `MAINT_ON` / `MAINT_OFF` / `ALARM_CLEAR` / `THRESHOLD_10` / `QUIET_1` / `SAMPLING_5` に絞っている
- `maintenance_awake` は bounded で、既定では数 cycle 後に自動で解除される
- duplicate command は `command_token` の recent ring で抑止し、same terminal result を乱発しない
- `sleepy_policy_set_node_id(...)` で runtime の node ID を差し替えられる
- `sleepy_policy_set_short_id(...)` で runtime の short ID を差し替えられる
- downlink backend は `radio_hal_service()` で差し込まれる前提

Kconfig:

- `CONFIG_EDGE_FABRIC_SLEEPY_USE_DEEP_SLEEP`
  `n` の既定では light sleep smoke を継続し、`y` では `esp_deep_sleep_start()` に入る
- `CONFIG_EDGE_FABRIC_SLEEPY_WAKE_INTERVAL_MS`
  light/deep sleep 共通の timer wake interval
- `CONFIG_EDGE_FABRIC_SLEEPY_ENABLE_RTC_PERSISTENCE`
  deep sleep wake 後に sequence / maintenance / short ID / recent token ring を復元する
- `CONFIG_EDGE_FABRIC_SLEEPY_RECENT_COMMAND_TOKEN_CACHE_SIZE`
  recent command token ring のサイズ。RTC state version に含め、サイズ変更時は古い RTC state を破棄する

default development backend:

- uplink を見ると 1 回だけ synthetic pending digest を返す
- tiny poll を見ると 1 回だけ synthetic sleepy command を返す
- 実機 downlink driver ではなく、binary on-air RX path smoke 用の最小 backend
- smoke 完了後は synthetic pending / command を再発しない
- `sleepy_leaf_backend_set_auto_smoke(false)` で auto success path を止められる
- `sleepy_leaf_backend_script_pending_digest(...)` / `sleepy_leaf_backend_script_compact_command(...)` で
  pending 0件や maintenance 系 command を scripted に差し込める

主な実装入口:

- `main/sleepy_leaf_main.c`
- `main/sleepy_policy.c`
