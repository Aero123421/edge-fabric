# node-sdk

`node-sdk` は sleepy leaf の最小 runtime です。

現時点の最小責務:

- JP-safe LoRa profile を適用する
- wake cycle を繰り返す最小 loop を持つ
- uplink 後に bounded RX window を開く
- pending command digest を受ける
- tiny poll を返す
- tiny command を受けて command_result を返す
- maintenance_awake の受信窓を切り替える

現段階の制約:

- JSON command は `contracts/protocol/sleepy-command-policy.json` の sleepy-safe command を優先する
- compact downlink は `ENP` service level と bounded mode token を前提にした最小実装
- `maintenance_sync` と maintenance-only command は `maintenance_awake` 前提
- `mode.set` の `maintenance_awake` / `deployed` で maintenance flag を切り替えられる
- `maintenance_awake` は bounded で、既定では数 cycle 後に自動で解除される
- duplicate command は小さな recent ring で抑止し、same terminal result を乱発しない
- `sleepy_policy_set_node_id(...)` で runtime の node ID を差し替えられる
- downlink backend は `radio_hal_service()` で差し込まれる前提

default development backend:

- uplink を見ると 1 回だけ synthetic pending digest を返す
- tiny poll を見ると 1 回だけ synthetic sleepy command を返す
- 実機 downlink driver ではなく、RX path smoke 用の最小 backend
- smoke 完了後は synthetic pending / command を再発しない
- `sleepy_leaf_backend_set_auto_smoke(false)` で auto success path を止められる
- `sleepy_leaf_backend_script_pending_digest(...)` / `sleepy_leaf_backend_script_compact_command(...)` で
  pending 0件や maintenance 系 command を scripted に差し込める

主な実装入口:

- `main/sleepy_leaf_main.c`
- `main/sleepy_policy.c`
