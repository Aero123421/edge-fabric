# SDK

SDK は app-facing semantic API を担当します。

- `publish_state`
- `emit_event`
- `issue_command`
- `observe_command`

`sendViaLora` のような bearer 露出 API は作りません。

現時点の最小実装は `pkg/sdk.LocalSiteRouterClient` と、より外向きの `pkg/fabric.Client` です。

- `pkg/sdk.OpenLocalSite(dbPath, sourceID)` で internal router を触らず local site を開けます
- `pkg/fabric.OpenLocal(dbPath, sourceID)` は typed state / event / sleepy command builder を提供します
- 戻り値は `PersistAck`
- local demo は `scripts/demo_local_router.py` にあります

sleepy node 向け command では少なくとも次を扱えるようにします。

- `service_level`
- `expected_delivery`
- `expires_at`
- `idempotency_key`

Go 本線 SDK は `pkg/sdk`、Python 側は `src/edge_fabric/sdk` です。

## Go typed entrypoint example

```go
fabric, err := fabric.OpenLocal("site.db", "app-01")
if err != nil {
    panic(err)
}
defer fabric.Close()

_, _ = fabric.PublishState(ctx, fabric.State{
    Source: "temp-01",
    Key:    "temperature.c",
    Value:  24.5,
})

_, _ = fabric.EmitEvent(ctx, fabric.Event{
    Source:   "motion-01",
    Type:     fabric.EventMotionDetected,
    Severity: fabric.Critical,
    Bucket:   3,
})
```
