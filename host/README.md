# Host

host 側は次を分離して実装します。

- `agent`
  session / keepalive / spool / ingress normalize
- `site-router`
  durable writer / dedupe / queue / routing

本線実装:

- Go `Site Router`: `internal/siterouter`
- Go `Host Agent`: `internal/hostagent`

参照実装:

- Python `Site Router`: `src/edge_fabric/host/site_router.py`
- Python `HostAgent`: `src/edge_fabric/host/agent.py`

最小責務:

- USB CDC frame decode
- ingress normalize
- Site Router relay
- router 不達時の spool
- heartbeat record
- spool diagnostics / flush

CLI 入口:

- `cmd/host-agent`
- `cmd/site-router`
