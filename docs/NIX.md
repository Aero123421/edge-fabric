# Nix Development Environment

This repository provides a Nix flake for Linux development and Raspberry Pi HIL runners.

The flake is intentionally focused on the host-side toolchain:

- Go mainline tools
- Python reference/test tools
- SQLite / jq / usbutils
- pyserial / pytest
- esptool when available in the selected nixpkgs set
- build helpers such as cmake / ninja / pkg-config

ESP-IDF is still treated as an external toolchain for now. That keeps first-pass Nix support small and reliable while avoiding a very large ESP-IDF closure. Use the normal Espressif install flow for `idf.py build`, `flash`, and `monitor` until the firmware toolchain is promoted into a dedicated Nix package.

## Usage

```bash
nix develop
go test ./...
python -S scripts/doctor.py --require-go
PYTHONPATH=src python -S -m unittest discover -s tests -v
```

With `direnv`:

```bash
direnv allow
```

The checked-in `.envrc` uses `use flake`.

## Raspberry Pi HIL Runner

On Raspberry Pi OS / Ubuntu aarch64:

```bash
nix develop
sudo usermod -aG dialout "$USER"
sudo reboot
```

After reboot, confirm USB access:

```bash
lsusb
python - <<'PY'
import serial.tools.list_ports
for port in serial.tools.list_ports.comports():
    print(port.device, port.description)
PY
```

Suggested split:

- Raspberry Pi 5: primary HIL runner, Site Router, Host Agent, gateway USB.
- Raspberry Pi 4: second Host Agent / second gateway host for multi-gateway observation.
- Desktop / laptop: development, firmware build, manual flash/debug.

## ESP-IDF

Nix shell does not currently provide `idf.py`. For firmware:

```bash
python scripts/doctor.py --require-go --require-idf
cd firmware/esp-idf/gateway-head
idf.py set-target esp32s3
idf.py build
```

Do the same under `firmware/esp-idf/node-sdk`.

## Lock File

If you want fully pinned Nix inputs, run:

```bash
nix flake lock
```

This will create `flake.lock`. Commit it when the selected nixpkgs revision has been verified on both `x86_64-linux` and `aarch64-linux`.
