# 02-critical-event

同じセンサーイベントを再送しても二重保存しないため、`IdempotencyKey` を指定します。

```go
_, err := client.EmitEvent(ctx, fabric.Event{
    Source:         "motion-01",
    Type:           fabric.EventMotionDetected,
    Severity:       fabric.Critical,
    Bucket:         3,
    IdempotencyKey: "boot-12:seq-88",
})
```

