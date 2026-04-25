# Security Modes

この repo の現時点の security posture は「実鍵や HIL を要求する運用保証」ではなく、runtime / contract で機械的に守れる gate を明示して積み上げる方針です。

## Modes

| Mode | 用途 | LoRa wire | declared size | heartbeat subject | RadioBudget | keys |
| --- | --- | --- | --- | --- | --- | --- |
| `dev` | local 開発と互換 smoke | legacy / JSON 互換を許容 | 許容 | legacy 推論を許容 | warning 相当 | test key のみ |
| `field-alpha` | 管理下の field trial | binary on-air を本線 | `allow_declared_lora_size_for_alpha=true` の明示 opt-in のみ | strict subject を要求 | enforce | test key のみ |
| `production` | release candidate / deploy gate | binary on-air 必須 | 禁止 | `subject_kind` / `subject_id` 必須 | enforce | test key 禁止、実鍵運用は HIL 外では未許可 |

契約の正本は `contracts/policy/security-modes.json` です。runtime の既定は `field-alpha` で、legacy compatibility が必要な smoke だけ `delivery.ingress_metadata.runtime_mode=dev` を明示します。release path では `runtime_mode=production` または payload の `production=true` を使い、Site Router の RoutePlanner は declared-size alpha path を拒否します。

## Code-Enforced Gates

- LoRa primary bearer に route_class 未指定の rich payload を暗黙投入しません。
- LoRa route は fixed binary on-air body で表現できる event / heartbeat / state、または field-alpha の declared-size opt-in だけを planning 対象にします。
- `production` mode では declared-size opt-in を `lora_declared_payload_forbidden_in_production` で block します。
- `production` / strict heartbeat は `subject_kind` と `subject_id` を必須にします。
- `sleepy_tiny_control` は compact command subset、short ID、payload fit、RadioBudget を enqueue 前に確認します。
- LoRa relay / mesh path は role、bearer、hop-limit、final target short ID、RadioBudget を RoutePlan に反映します。

## RadioBudget

RadioBudget の契約は `contracts/policy/radio-budget.json` です。実装は `internal/protocol/jp` の airtime 推定と Site Router の RoutePlanner guard で、RoutePlan の `detail.radio_budget` に以下を残します。

- `profile`
- `body_bytes`
- `overhead_bytes`
- `total_payload_bytes`
- `estimated_airtime_ms`
- `max_airtime_ms`
- `decision`

超過時は `radio_airtime_budget_exceeded` で `route_blocked` になります。現時点では CAD/LBT 実測、channel busy accounting、実 gateway の duty-cycle ledger は HIL なしでは保証できないため、コードで守れる per-packet airtime gate までを production readiness gate とします。

## Not Claimed Yet

- 実鍵 provisioning、secure element、key rotation、revocation。
- 実 SX1262 の CAD/LBT 実測と channel occupancy ledger。
- HIL による gateway / node / relay の end-to-end security validation。
- OTA / maintenance transfer の production security guarantee。
