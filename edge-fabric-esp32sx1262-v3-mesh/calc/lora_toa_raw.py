#!/usr/bin/env python3
"""Generate raw LoRa airtime matrices used by the deep-dive addendum.

Assumptions:
- Explicit header
- CRC enabled
- Preamble length = 8 symbols
- Coding rate = 4/5
- Low-data-rate optimization auto-enabled for SF11/SF12 at 125 kHz
- `payload_bytes` means raw radio payload bytes, not JSON body size and not LoRaWAN FRMPayload size

This script is intentionally simple so it can be audited and rewritten in Codex easily.
"""

import csv
import math
from pathlib import Path

def lora_toa_ms(payload_bytes, sf=7, bw=125000, cr=1, preamble=8, crc=1, ih=0, de=None):
    if de is None:
        de = 1 if (sf >= 11 and bw == 125000) else 0
    tsym = (2**sf) / bw
    tpreamble = (preamble + 4.25) * tsym
    payload_symb = 8 + max(
        math.ceil(
            (8*payload_bytes - 4*sf + 28 + 16*crc - 20*ih) /
            (4*(sf - 2*de))
        ) * (cr + 4),
        0
    )
    tpayload = payload_symb * tsym
    return (tpreamble + tpayload) * 1000

def max_payload_under(ms, sf, bw=125000, cr=1):
    best = -1
    for p in range(0, 256):
        if lora_toa_ms(p, sf=sf, bw=bw, cr=cr) <= ms:
            best = p
        else:
            break
    return best

out = Path(__file__).resolve().parent

for bw in [125000, 250000]:
    with (out / f"lora_toa_matrix_bw{bw//1000}.csv").open("w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, fieldnames=["bw_khz", "sf", "payload_bytes", "toa_ms"])
        w.writeheader()
        for sf in range(7, 13):
            for p in range(0, 129):
                w.writerow({
                    "bw_khz": bw // 1000,
                    "sf": sf,
                    "payload_bytes": p,
                    "toa_ms": round(lora_toa_ms(p, sf=sf, bw=bw), 3),
                })

with (out / "lora_airtime_caps.csv").open("w", newline="", encoding="utf-8") as f:
    w = csv.DictWriter(f, fieldnames=["bw_khz", "sf", "max_payload_under_400ms_bytes", "max_payload_under_250ms_bytes", "toa_0_bytes_ms"])
    w.writeheader()
    for bw in [125000, 250000]:
        for sf in range(7, 13):
            w.writerow({
                "bw_khz": bw // 1000,
                "sf": sf,
                "max_payload_under_400ms_bytes": max_payload_under(400, sf=sf, bw=bw),
                "max_payload_under_250ms_bytes": max_payload_under(250, sf=sf, bw=bw),
                "toa_0_bytes_ms": round(lora_toa_ms(0, sf=sf, bw=bw), 3),
            })
