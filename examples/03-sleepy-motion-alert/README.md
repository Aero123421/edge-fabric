# 03-sleepy-motion-alert

sleepy battery node は relay せず、motion event を compact event として送る profile を使います。

```go
err := fabric.RegisterDeviceProfile(
    ctx,
    client,
    "motion-01",
    fabric.MotionSensorBatteryProfile(),
    201,
    fabric.WithRole("sleepy_leaf"),
    fabric.WithPrimaryBearer("lora"),
    fabric.WithFallbackBearer("ble_maintenance"),
)
```

