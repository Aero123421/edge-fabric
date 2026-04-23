# Project-Derived Principles

このリポジトリは特定プロジェクト専用品ではありません。  
ただし、以下の既存ドキュメント群から汲み取れる良い設計原則を、**汎用化して再構成**しています。

## 使った考え方

- **[P1]** `poc3/docs/01-design-principles.md`  
  現場で最終判断を持つ / state-event-command 分離 / powered backbone / serviceability first

- **[P2]** `poc3/docs/02-system-architecture-and-runtime-boundaries.md`  
  runtime boundary / trust boundary / local safety vs central control plane

- **[P3]** `poc3/docs/03-node-families-and-field-topology.md`  
  board名ではなく role と capability を主語にする / direct Wi-Fi first, mesh second

- **[P4]** `poc3/docs/04-identity-data-and-protocol.md`  
  `hardware_id`, `installation`, `event_id`, `command_id`, `session_id`, `seq_local`

- **[P5]** `poc3/docs/05-site-controller-local-db-and-queue.md`  
  latest state / ledger / outbox の分離, local-first, recovery

- **[P6]** `poc3/docs/adr/ADR-0003-role-fixed-transport-policy.md`  
  packetごとの自由切替より、role固定 transport を優先

- **[P7]** `tmp/lora-wifi-hybrid-optimization-plan-2026-04-14.md`  
  LoRaは sparse / critical / battery、Wi-Fiは detailed / command / OTA

- **[P8]** `tmp/protocol-identity-and-approval-spec-2026-04-14.md`  
  event / command lifecycle の厳密な扱い

- **[P9]** `tmp/split-brain-reconciliation-2026-04-14.md`  
  local-first / central-eventual での reconcile

- **[P10]** `tmp/heartbeat-deep-dive-followup-2026-04-20.md`  
  heartbeat を Node / Gateway / Host / Summary に分ける発想

## このリポジトリでやった汎用化

上の資料から、以下は**汎用 core に残す**判断をしました。

- local-first
- single logical writer
- state / event / command / heartbeat 分離
- `event_id` / `command_id` / `session_id` / `seq_local`
- role-fixed transport policy
- latest state / ledger / outbox 分離
- multi-host / multi-gateway を前提にした dedupe と routing

逆に、以下は**特定業務ロジックとして core から外す**判断をしました。

- 業務専用の状態語彙
- 特定アラート種別
- 特定現場運用フロー
- 特定UI文言
- 特定クラウド事業者の control plane 依存

## 位置づけ

このファイルは「どの考え方が既存プロジェクトから来たか」を説明するものであり、  
公開一次情報の代わりではありません。  
ハード仕様や規制値は `sources/official-sources.md` の一次情報を正本とします。
