# ADR-0001: Single Logical Writer

## Status
Accepted

## Context
複数 gateway、複数 host、複数 client を許可したいが、  
それぞれが別個に durable 正本を書くと split-brain になりやすい。

## Decision
1 site における durable ledger / latest_state projection / command lifecycle を確定する主体は、  
**1 つの logical writer** とする。  
物理 ingress は複数でよい。

## Consequences
- multi-host / multi-gateway に伸ばしやすい
- command の二重実行を抑えやすい
- HA / failover 設計は別途必要
