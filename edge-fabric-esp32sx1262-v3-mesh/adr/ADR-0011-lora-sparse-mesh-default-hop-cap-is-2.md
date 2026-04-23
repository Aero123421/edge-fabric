# ADR-0011 LoRa sparse mesh default hop cap is 2

Decision: production default の LoRa relay hop cap は 2 とする。3 は experimental。

Why:
- hop 増加は effective channel occupancy を直線的に増やす
- relay header で app body が減る
- JP-safe operation で保守的に始めるため
