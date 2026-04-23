# ADR-0002: Role-Fixed Transport Policy

## Status
Accepted

## Context
LoRa と Wi-Fi を packet ごとに自由に切り替えると、現場デバッグと説明責任が難しくなる。

## Decision
- LoRa は `battery / sparse / critical / long-range`
- Wi-Fi は `powered / detailed / command / bulk`
- bounded failover と critical redundant send は許可
- packet 単位の自由 route switching は標準にしない

## Consequences
- 原因切り分けが容易
- battery life 見積もりがしやすい
- transport policy を role lease へ落とし込みやすい
