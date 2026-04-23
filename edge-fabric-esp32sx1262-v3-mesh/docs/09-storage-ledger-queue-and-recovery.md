# Storage, Ledger, Queue, And Recovery

## 1. durable 正本をどこに置くか

この repo では durable 正本を **Site Router** に置く。  
Node / Gateway Head / Host Agent は一時保持をしてよいが、正本にはしない。

## 2. 最低限分けるべきストレージ

## 2.1 latest_state
用途:
- UI の current view
- quick query
- status summary

### 特徴
- projection
- append-only ではない
- rebuild 可能であること

## 2.2 event_ledger
用途:
- append-only event
- audit
- backfill
- dedupe 基準

### 特徴
- `UNIQUE(event_id)`
- current view の代わりに使わない

## 2.3 command_ledger
用途:
- `issued` command 記録

## 2.4 command_execution
用途:
- accepted / executing / succeeded / failed / rejected / expired

## 2.5 outbox_queue
用途:
- central / remote / target host / target node への再送待ち
- retry
- dead-letter
- reconnect flush

## 3. 推奨 v1 ストレージ

v1 の local-first 実装では、Site Router は **SQLite** を第一候補にする。  
理由:

- Raspberry Pi / PC で扱いやすい
- durable single-writer model と相性がよい
- field appliance に向く
- Codex でも早く着手しやすい

### 3.1 推奨分割
- `site_state.db`
- `site_ledger.db`

### 3.2 推奨設定
- WAL
- full sync for ledger / queue
- checkpoint policy を固定
- latest_state は ledger から rebuild 可能

## 4. queue state machine

```text
created -> queued -> leased -> sending -> sent_ok -> acked
                             \-> queued
                             \-> dead
```

## 5. lease の意味

queue item の `lease` は「今この worker / process が送信責任を握っている」ことを示す。  
これにより crash recovery を実装しやすくする。

## 6. recovery

起動時 recovery の最低要件:

- `leased` / `sending` で止まった queue item を回収
- stale lease を剥がす
- gateway / host の再接続を待って再送
- latest_state を必要に応じて rebuild 可能

## 7. split-brain の扱い

この repo は `local-first / central-eventual` の考え方を採る。  
したがって split-brain を**前提として扱う**。

### 7.1 原則
- local safety / local state は継続
- reconnect 後に ledger backfill
- current state 採用順は `occurred_at + session_id + seq_local`

### 7.2 やってはいけないこと
- `received_at` が新しいから current を上書きする
- retry のたびに ID を変える
- current table だけを監査の正本にする

## 8. multi-host ingest

Host Agent は複数存在してよい。  
ただし Site Router だけが durable writer となる。

### 8.1 host spool
Host Agent は短期 spool を持ってよい。  
用途:
- Site Router 一時不達時の buffer
- USB reconnect の吸収

### 8.2 router ingest
Site Router は host ingest を idempotent に受ける必要がある。

## 9. observability

最低限必要な queue metrics:

- queued_count
- leased_count
- sending_count
- retry_count
- dead_count
- oldest_queued_age_ms
- queue_lag_p95

## 10. 保持と圧縮

### 10.1 latest_state
projection なので compaction 前提。

### 10.2 event_ledger
append-only の保持ポリシーを持つ。  
ただし audit / support / field replay の都合を考慮して retention を決める。

### 10.3 telemetry
detailed telemetry は別 retention policy を持ってよい。  
critical event と同じ扱いにしない。

## 11. 主な根拠

- `[P5]` latest/ledger/outbox separation
- `[P8]` ordering and idempotency
- `[P9]` reconcile
