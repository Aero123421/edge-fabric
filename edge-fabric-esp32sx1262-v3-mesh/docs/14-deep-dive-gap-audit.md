# Deep Dive Gap Audit

この版では、前回版までに残っていた主要な gap を棚卸しし、どこを埋めたかを明記します。

## 1. 前回版の主な不足

### GAP-1: 自立 mesh が主役ではなかった
前回版は multi-gateway / multi-host / hybrid transport は扱っていたが、  
**自立 mesh** は中心テーマではなかった。

### GAP-2: deep sleep node と relay の境界が弱かった
“battery node は relay に向かない” という方向性はあったが、  
mesh-first 設計として十分に強く固定していなかった。

### GAP-3: Wi-Fi mesh と LoRa mesh の役割分担が弱かった
“Wi-Fi と LoRa を使い分ける” までは書いていたが、  
**Wi-Fi backbone / LoRa overlay** という構図が明文化されていなかった。

### GAP-4: 複数 root / 複数 server の site 像が薄かった
multi-host はあったが、multi-domain / Fabric Spine の語彙が足りなかった。

### GAP-5: LoRa relay 時の payload / hop 影響が弱かった
1 hop 前提の payload cap はあったが、relay 時の overhead 差と occupancy 増加が弱かった。

### GAP-6: サーボや人感など device pattern への落とし込みが弱かった
generic foundation を目指すと言いつつ、具体的な device archetype が少なかった。

---

## 2. 今回追加したもの

### FIX-1
`docs/26` で mesh-first architecture を主語にした。

### FIX-2
`docs/27` で Wi-Fi mesh backbone / root / domain policy を追加した。

### FIX-3
`docs/28` で LoRa sparse mesh overlay と relay budget を追加した。

### FIX-4
`docs/29` で hybrid route cost model を追加した。

### FIX-5
`docs/30` で discovery / neighbor table / hysteresis を追加した。

### FIX-6
`docs/31` で multi-domain / multi-server / Fabric Spine を追加した。

### FIX-7
`docs/32` で scale guidance を追加した。

### FIX-8
`docs/33` で device pattern を具体化した。

### FIX-9
`docs/34` と `calc/` で per-hop payload / occupancy を追加した。

### FIX-10
ADR-0009〜0015 で mesh-first decisions を固定した。

---

## 3. なお残る open areas

### OPEN-1
Wi-Fi LR mode を core optional に入れるかは将来判断。 `[S9]`

### OPEN-2
Site Router の active/standby は将来段階。

### OPEN-3
LoRa relay beacon interval の最適値は現場計測で詰める。

### OPEN-4
2 relay beyond の実用性は site-specific。

### OPEN-5
Wi-Fi mesh root designation vs self-organized の既定値は運用パターンで微調整余地あり。

---

## 4. Audit conclusion

今回の版で、repo は
- hybrid transport spec
から
- **mesh-first generic edge fabric spec**
へ進化した。

特に、
- deep sleep leaf は relay にしない
- powered node が骨格を作る
- Wi-Fi mesh が backbone
- LoRa は sparse overlay
- multi-domain / multi-server は spine で束ねる
が、今回の版の中心的な完成点です。
