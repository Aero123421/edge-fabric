# ADR-0004: Local-First Ledger

## Status
Accepted

## Context
cloud / central を唯一の正本にすると、回線断や ingest 遅延で current state と監査が壊れやすい。

## Decision
site 内に local ledger, latest_state projection, outbox queue を持つ。  
central / cloud は optional replication target とする。

## Consequences
- field appliance として強くなる
- recovery / backfill が実装しやすい
- central-only より実装点数は増える
