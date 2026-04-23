# ADR-0003: JP Region Policy First

## Status
Accepted

## Context
SX1262 の generic datasheet と、日本向け認証条件は一致しない。  
generic 値をそのまま production に持ち込むのは危険。

## Decision
日本向け production build は `RegionPolicy::JP` を mandatory にし、  
frequency / output / antenna / carrier-sense を production guard として持つ。

## Consequences
- debug build と production build を分ける必要がある
- repo 初期段階から region policy layer が必要になる
- field deployment の説明責任が上がる
