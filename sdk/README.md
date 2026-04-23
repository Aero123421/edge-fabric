# SDK

SDK は app-facing semantic API を担当します。

- `publish_state`
- `emit_event`
- `issue_command`
- `observe_command`

`sendViaLora` のような bearer 露出 API は作りません。

現時点の最小実装は `LocalSiteRouterClient` です。

- `router: SiteRouter` が必須
- 戻り値は `PersistAck`
- local demo は `scripts/demo_local_router.py` にあります

sleepy node 向け command では少なくとも次を扱えるようにします。

- `service_level`
- `expected_delivery`
- `expires_at`
- `idempotency_key`

Go 本線 SDK は `pkg/sdk`、Python 側は `src/edge_fabric/sdk` です。
