# 01-basic-state

外向き `pkg/fabric` SDK の最小例です。アプリは bearer を指定せず、状態として発行します。

```go
client, err := fabric.OpenLocal("site.db", "app-01")
if err != nil {
    return err
}
defer client.Close()

_, err = client.PublishState(ctx, fabric.State{
    Source: "temp-01",
    Key:    "temperature.c",
    Value:  24.5,
})
```

