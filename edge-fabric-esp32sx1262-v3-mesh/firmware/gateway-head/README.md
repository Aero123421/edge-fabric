# firmware/gateway-head

USB で Host Agent につながる radio head firmware の予定地。

## 役割
- SX1262 driver
- LoRa ingress / egress
- minimal hop ack support
- USB CDC framing
- gateway heartbeat
- ingress metadata export

## 持たせないもの
- durable writer
- project business logic
- site-wide state projection
