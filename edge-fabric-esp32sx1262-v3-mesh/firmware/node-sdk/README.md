# firmware/node-sdk

Node app が使う SDK の予定地。

## public API の主語
- `publish_state`
- `emit_event`
- `on_command`
- `reply_command_result`
- `report_heartbeat`
- `request_lease`
- `send_manifest`

## SDK が内部でやること
- route class 決定
- local queue
- summary codec 適用
- sleepy lifecycle
- bearer 選択
- over-cap 処理

## 守るべきルール
- bearer 名を app API に出さない
- sleepy node を relay にしない
- GPIO予約は `docs/02` を守る
- LoRa payload cap は `docs/16` / `docs/34` を守る
