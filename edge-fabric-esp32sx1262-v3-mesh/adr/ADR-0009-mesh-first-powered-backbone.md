# ADR-0009 Mesh-first powered backbone

Decision: この repo は Wi-Fi mesh powered backbone を first-class architecture とする。

Why:
- powered node は Wi-Fi mesh の self-organized / self-healing を活かせる
- LoRa を backbone 主体にすると payload / latency / occupancy が厳しい
- battery leaf と powered backbone を分離した方が generic foundation として強い
