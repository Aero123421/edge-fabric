# host/site-router

1 site の durable writer になる予定地。

## 役割
- event_ledger
- latest_state projection
- command lifecycle
- queue / DLQ
- dedupe
- route decision
- multi-root / multi-gateway merge
- client pub/sub
- fabric summary
