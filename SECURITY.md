# Security Policy

脆弱性報告は public issue ではなく、まず private channel で共有してください。

推奨手段:

- GitHub Security Advisory が使える環境では Security Advisory を使う
- 使えない環境では、リポジトリ管理者へ private に連絡してから公開 issue を作る
- 連絡先が未整備な間は、脆弱性の詳細を含まない最小限の issue で「private contact が必要」とだけ伝える

受け付け対象:

- replay / duplicate 耐性の破り方
- command idempotency 破壊
- queue recovery / dead-letter 破壊
- JP-safe guard の迂回
- app-facing API への transport detail 漏えい

対象外:

- 未実装 scaffold の機能不足そのもの
- 仕様どおりの制約による接続失敗
- 公開されている fixture の内容に関する一般的な質問

現時点で security review の重点は次です。

- duplicate / replay 耐性
- command idempotency
- queue recovery
- JP-safe guard を迂回できないこと
- app-facing API に transport detail を漏らさないこと

まだ early-stage implementation なので、breaking fix が入る可能性があります。
