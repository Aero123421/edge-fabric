# 05-powered-servo-controller

powered servo controller は `local_control` を使い、LoRa realtime control を profile で禁止します。

```go
err := fabric.RegisterDeviceProfile(
    ctx,
    client,
    "servo-01",
    fabric.PoweredServoControllerProfile(),
    0,
    fabric.WithPrimaryBearer("wifi"),
)
```

