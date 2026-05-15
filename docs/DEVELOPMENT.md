# 開発・検証コマンド

このページは CI とローカルで同じ手順を取りやすくするための最短コマンドをまとめます。

## 0. まず確認するコマンド

PowerShell:

```powershell
git status --short
python .\scripts\doctor.py
```

Bash:

```bash
git status --short
python ./scripts/doctor.py
```

`doctor.py` は repo 配置・fixture 整合・レイアウト健全性を見ます。Go / ESP-IDF の厳密チェックは以下の track オプションを使います。

```powershell
python .\scripts\doctor.py --track go
python .\scripts\doctor.py --track firmware
python .\scripts\doctor.py --track all
python .\scripts\doctor.py --require-go
python .\scripts\doctor.py --require-idf
```

```bash
python ./scripts/doctor.py --track go
python ./scripts/doctor.py --track firmware
python ./scripts/doctor.py --track all
python ./scripts/doctor.py --require-go
python ./scripts/doctor.py --require-idf
```

## 1. Python reference track

PowerShell:

```powershell
python -m venv .venv
.\.venv\Scripts\Activate.ps1
python -m pip install -e .
python .\scripts\doctor.py
python -m unittest discover -s tests -v
```

Bash:

```bash
python -m venv .venv
source .venv/bin/activate
python -m pip install -e .
python ./scripts/doctor.py
python -m unittest discover -s tests -v
```

## 2. Go mainline track

PowerShell:

```powershell
python .\scripts\doctor.py --require-go
go test ./...
go build ./cmd/...
go run .\cmd\sleepy-cycle-demo
```

Bash:

```bash
python ./scripts/doctor.py --require-go
go test ./...
go build ./cmd/...
go run ./cmd/sleepy-cycle-demo
```

CI では Linux のみで `go test -race ./internal/... ./pkg/...` を追加実行しています。

## 3. ESP-IDF firmware track

先に `ESP-IDF` / `idf.py` を入れた環境で実行します。`idf.py` を使う前に対象 app ディレクトリへ移動してください。

PowerShell (gateway-head 例):

```powershell
cd .\firmware\esp-idf\gateway-head
python ..\..\..\scripts\doctor.py --require-go --require-idf
idf.py set-target esp32s3
idf.py build
```

Bash (node-sdk 例):

```bash
cd firmware/esp-idf/node-sdk
python ../../../scripts/doctor.py --require-go --require-idf
idf.py set-target esp32s3
idf.py build
```

## 4. CI との対応

`.github/workflows/ci.yml` は次の順で実行されます。

- Python: `python scripts/doctor.py` → `python -m unittest discover -s tests -v` → `python -m unittest tests/test_contracts.py -v`
- Go: `python scripts/doctor.py --require-go` → `go test ./...` → `go build ./cmd/...`
- Linux 限定: `go test -race ./internal/... ./pkg/...`
- firmware: `idf.py build` の default と strict real-backend matrix

## 5. release hardening の既知制約

現在、次は `release candidate` の未完了項目です。

- 実機 HIL（gateway/node/relay）が未実装
- 正式プロビジョニング / real key / anti-replay は未完了
- 実運用想定のセキュリティゲートは docs で `production` を明示しつつ、実装は段階的

最新状況は [docs/KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md) と [docs/SUPPORT_MATRIX.md](SUPPORT_MATRIX.md) を参照してください。
*** End Patch
