# ADR-0014 Summary codec is mandatory for LoRa fallback

Decision: summary codec を持たない message class は LoRa fallback しない。

Why:
- over-cap payload を無理に fragmentation しない
- payload discipline を保つ
- LoRa relay path の無駄な失敗を避ける
