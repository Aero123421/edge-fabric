# 11-route-explain

route がなぜ通る/落ちるかは CLI で説明できます。

```powershell
go run .\cmd\edge-fabric explain-route -seed-fixtures -fixture .\contracts\fixtures\command-sleepy-threshold-set.json
```

on-air payload の切り分けには `decode-onair` を使います。

```powershell
go run .\cmd\edge-fabric decode-onair -hex <hex-frame>
```
