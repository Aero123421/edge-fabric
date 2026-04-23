# ADR-0015 Immediate control stays off LoRa mesh

Decision: immediate_local / closed-loop control は LoRa mesh へ failover しない。

Why:
- latency / jitter / wake behavior / relay uncertainty
- actuator safety と user expectation を壊さないため
