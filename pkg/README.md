# pkg

このディレクトリは、Go 側の public-ish package を置く場所です。

- `contracts`
  envelope / manifest / lease 型と validation
- `sdk`
  low-level client API と local Site Router entrypoint
- `fabric`
  外部アプリ向けの typed SDK entrypoint。`PublishState`, `EmitEvent`, sleepy tiny command builder, device profile registration を提供します
