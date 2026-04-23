# Contributing

本線は `Go + ESP-IDF` です。`Python` は参照実装なので、PR ではどのトラックに触れたかを明記してください。

## 方針

- 小さい slice で変更してください
- bearer 名を app-facing API に漏らさないでください
- `Site Router` を durable single writer とする前提を崩さないでください
- `JP-safe` 制約は hardening ではなく core 条件として扱ってください

## 変更前の確認

### Go mainline に触る場合

PowerShell:

```powershell
python .\scripts\doctor.py --require-go
go test ./...
go run .\cmd\site-router -op doctor
```

### Python reference に触る場合

```powershell
python -m pip install -e .
python .\scripts\doctor.py
python -m unittest discover -s tests -v
```

### ESP-IDF firmware に触る場合

`ESP-IDF 5.2+` 環境で対象 app ディレクトリへ移動して、最低限 `idf.py build` を通してください。

対象:

- `firmware/esp-idf/gateway-head`
- `firmware/esp-idf/node-sdk`

この workspace では `idf.py` が入っていない場合があります。
その場合でも、少なくとも次は崩さないでください。

- `python .\scripts\doctor.py`
- contract artifact と fixture の整合
- `docs/KNOWN_LIMITATIONS.md` の更新
- backend seam / default backend / support level の説明更新

## PR の期待値

- 変更理由が分かること
- どのトラックを触った PR かが明記されていること
- 契約変更がある場合は fixture と protocol artifact が更新されていること
- 新しい durable behavior を入れる場合は recovery / dedupe のテストがあること
- LoRa 関連変更は payload cap と JP-safe profile を壊していないこと

## 必須チェック

- `Go mainline`
  `go test ./...`
- `Python reference`
  `python -m unittest discover -s tests -v`
- `ESP-IDF firmware`
  変更した app の `idf.py build`

`idf.py build` をこの環境で回せなかった場合は、その事実を PR 説明に必ず明記してください。

target / feature の公開状態を変える変更では、[docs/SUPPORT_MATRIX.md](docs/SUPPORT_MATRIX.md) と
[docs/KNOWN_LIMITATIONS.md](docs/KNOWN_LIMITATIONS.md) を必ず見直してください。

`gofmt` 相当の整形は必須です。CI で再現できるコマンドを PR 説明に添えてください。

公開用 source zip を作るときは clean worktree で `python .\scripts\export_clean_repo.py` を使ってください。編集中の確認だけなら `--allow-dirty` を付けます。

## 今の優先順位

1. contracts / protocol freeze
2. Site Router durable core
3. direct LoRa / Wi-Fi direct slice
4. SDK public beta
5. mesh / relay / hybrid
