# LoRa JP Safe Profiles, Airtime, And Payload Budget

## 1. まず分けるべきもの

この文書では、次の 3 つを分けます。

1. **module の一般能力**
2. **日本で認証された条件**
3. **この fabric が production default として許可する条件**

この 3 つを混ぜると、「SX1262 は 22dBm まで出せるから日本でもそう使える」  
のような誤解が起きます。

---

## 2. module の一般能力

Wio-SX1262 module datasheet では、一般仕様として

- frequency range: 862–930 MHz
- output power: 22 dBm max
- sleep current: 1.62 µA
- RX current: 7.6 mA @ BW125kHz
- TX current: 125 mA @ 22 dBm
- sensitivity: -136.73 dBm @ SF12, BW125kHz

が示されています。 `[S4]`

これは **module の広域仕様**であり、日本向け production のそのままの許可値ではありません。

---

## 3. 日本向け認証条件

Wio-SX1262 の日本向け認証資料では、

- LoRa 125 kHz: 920.6–928.0 MHz
- LoRa 250 kHz: 920.7–927.9 MHz
- maximum output power: 10.000 mW rated
- antenna condition: 3 antennas, max gain 8 dBi

が示されています。 `[S8]`

したがって、この repo では production build において:

- `RegionPolicy::JP` を mandatory にする
- 認証条件を超える power / antenna / channel plan を UI から安易に変更させない
- “global 862–930 / 22 dBm” を production default にしない

という方針を取ります。

---

## 4. ARIB STD-T108 が追加で意味すること

ARIB STD-T108 では、carrier sense と emission time control の条件が規定されています。  
例えば 922.4–923.4 MHz では、128 µs 以上の carrier sense と、  
1 unit radio channel で emission < 400 ms / arbitrary one hour sum <= 360 s、  
2 unit で < 200 ms、3–5 unit で < 100 ms といった条件が記載されています。 `[S16]`

さらに carrier sense level は -80 dBm at antenna input で規定されており、  
一定条件下の response だけは carrier sense 省略の例外があります。 `[S16]`

この repo のポイントはここです。

### 4.1 重要
**LoRa frame の airtime は、payload 設計そのものの制約**です。

“payload が radio buffer に入るか” だけでは不十分で、

- emission time
- LBT / carrier sense
- collision probability
- gateway single-channel occupancy
- battery wake budget

も同時に満たす必要があります。

---

## 5. CAD をそのまま carrier sense とみなさない

Semtech の CAD 資料では、CAD は **energy-efficient preamble detection** を主目的としており、  
preamble detection は full carrier sense ではないと説明されています。 `[S18]`

一方、ARIB STD-T108 の carrier sense は受信電力レベルで規定されています。 `[S16]`

この repo の設計判断:
- production `RegionPolicy::JP` では、**CAD-only を carrier sense 実装とみなさない**
- JP build の LBT は、energy detect / RSSI 判定を含む設計にする
- CAD は補助用途（LoRa preamble 検知 / low-power listen）には使ってよい
- ただし JP 規制準拠の唯一根拠にはしない

---

## 6. v1 production default の JP safe policy

この repo は “全部の組み合わせを許す” のではなく、  
**v1 production safe profile** を狭く取ります。

### 6.1 channel plan の方針
- v1 default は **1-unit channel** を前提にする
- `922.4–923.4 MHz` 帯を primary production window とする
- 920.6–922.2 の運用は optional / site-specific とし、default にしない

理由:
- 128 µs class の carrier sense ルールに寄せる
- profile 設計を単純化する
- single-channel head の実装複雑度を抑える

### 6.2 modulation profile の方針
v1 production default では、以下を採用します。

| Profile | BW | SF | Intended use | Total radio payload cap |
|---|---:|---:|---|---:|
| `JP125_LONG_SF10` | 125 kHz | 10 | critical alert / join-lite / summary heartbeat | 24 B |
| `JP125_BAL_SF9` | 125 kHz | 9 | sparse event / state / result | 48 B |
| `JP250_FAST_SF8` | 250 kHz | 8 | short-range powered fallback / bridge uplink | 80 B |
| `JP250_CTRL_SF9` | 250 kHz | 9 | compact control / powered fallback | 64 B |

### 6.3 v1 で default から外すもの
- `SF11 @ 125 kHz`
- `SF12 @ 125 kHz`
- general-purpose fragmentation
- “とりあえず payload が大きいから 250 kHz で何とかする” という発想

---

## 7. なぜ SF11 / SF12 を default から外すのか

この addendum の `calc/` に raw LoRa airtime table を入れています。  
その前提は以下です。

- explicit header
- CRC on
- preamble = 8
- coding rate = 4/5
- low data rate optimization auto-enabled for SF11/SF12 at 125 kHz

この近似計算では、125 kHz で

- SF11 の **0-byte raw payload** でも約 331.8 ms
- SF12 の **0-byte raw payload** で約 663.6 ms

になります。  
つまり、1-unit channel / 400 ms ceiling の profile では、

- SF11 は実 payload をほとんど積めない
- SF12 はそもそも ceiling を超えやすい

という結論になります。

したがってこの repo では、  
**“長距離が欲しいからとりあえず SF12”** を production default にしません。

---

## 8. payload budget を radio payload で考える

ここはよく混乱します。

- app payload
- fabric envelope
- AEAD tag
- raw LoRa radio payload

は別物です。

この repo で profile 表の `Total radio payload cap` は、  
**raw LoRa payload 全体のバイト数**を意味します。

例えば `JP125_LONG_SF10` の cap が 24 B なら、
- header
- ack piggyback
- auth tag
- app body

を全部足して 24 B 以内に収める必要があります。

---

## 9. compact envelope を引いた実 app body

この repo の compact LoRa envelope は、  
最小 uplink で 14 B、最小 downlink control で 16 B を仮定します。  
詳細は `docs/17-compact-wire-format-payload-shapes-and-overhead.md` を参照。

すると概算上の app body は以下です。

| Profile | Total cap | Uplink min overhead | App body cap (uplink) |
|---|---:|---:|---:|
| `JP125_LONG_SF10` | 24 B | 14 B | 10 B |
| `JP125_BAL_SF9` | 48 B | 14 B | 34 B |
| `JP250_FAST_SF8` | 80 B | 14 B | 66 B |
| `JP250_CTRL_SF9` | 64 B | 14 B | 50 B |

この数字が意味するのは明確です。

### 9.1 重要
LoRa long-range profile では、**“意味の要約” しか送れない**ことが多い。

つまり、
- 画像
- 長文ログ
- JSON 全文
- UUID を何個も含むメッセージ
- 詳細診断

は載せる前提にしてはいけません。

---

## 10. Semtech の LoRaWAN ベンチマークも同じ方向を示す

machineQ / Comcast の design guide では、125 kHz uplink での  
**11-byte payload** の time-on-air 例として

- SF7: 61 ms
- SF8: 103 ms
- SF9: 185 ms
- SF10: 371 ms

が示されています。 `[S19]`

これは LoRaWAN の app payload benchmark なので raw custom LoRa と完全一致ではありません。  
ただし、示している本質は同じです。

### 10.1 本質
**payload が小さくても、SF が上がると airtime は急に重くなる。**

single-channel gateway/head と組み合わせるなら、
この影響はさらに大きく感じます。

---

## 11. v1 の payload policy

この repo では payload を 4 区分します。

## 11.1 LoRa native
最初から compact で、LoRa long profile に自然に収まるもの。

例:
- alert code
- compact heartbeat
- compact command result
- tiny sensor sample

## 11.2 dual-shape
Wi-Fi full shape と LoRa summary shape の両方を持つもの。

例:
- alarm full detail (Wi-Fi)
- alarm summary (LoRa)

## 11.3 Wi-Fi only
summary も定義しないもの。  
LoRa fallback 不可。

例:
- bulk log
- file
- OTA
- waveform dump
- long config blob

## 11.4 fragmentation candidate
理論上 fragment できるが v1 default では使わないもの。

---

## 12. airtime budget の実務ルール

### 12.1 MUST
LoRa send 前に、chosen profile の `total radio payload cap` を超えていないことをチェックする。

### 12.2 MUST
超えた場合は:
1. summary codec があるなら summary に落とす
2. Wi-Fi bearer が許可されていれば Wi-Fi に回す
3. どちらも無理なら priority policy に従って queue / reject / coalesce

### 12.3 MUST NOT
`bulk` を LoRa に fallback しない。

### 12.4 SHOULD
normal telemetry は coalesce し、古い状態を何件も LoRa に流さない。

---

## 13. profile adaptation の考え方

この repo は full LoRaWAN ADR clone を目指しません。  
代わりに、**lease-driven profile selection** を使います。

### 13.1 Node が自律的に毎 packet で自由に変えない
デバッグ不能になるため。

### 13.2 Site Router / policy が段階的に変える
例:
- `JP125_BAL_SF9`
- 失敗が増えたら `JP125_LONG_SF10`
- 短距離で安定したら `JP250_FAST_SF8`

### 13.3 hysteresis を持つ
1 packet 成功しただけで profile を上げ下げしない。

---

## 14. battery node と powered node で profile の使い方を変える

### 14.1 battery node
- default: `JP125_BAL_SF9`
- poor link: `JP125_LONG_SF10`
- 250 kHz は default にしない

理由:
- battery node は “とにかく届く compact uplink” が優先
- Wi-Fi を常時使えない
- fragment や大きい payload は不向き

### 14.2 powered node / bridge
- default: Wi-Fi primary
- LoRa fallback summary: `JP250_FAST_SF8` or `JP250_CTRL_SF9`
- site specific に `JP125_BAL_SF9` も可

理由:
- powered node は Wi-Fi full path が本命
- LoRa は summary / degraded path
- 250 kHz short-range fast path を使いやすい

---

## 15. この repo の結論

- **LoRa payload 制約は “あとで考える” ものではなく、最初に決めるべき API 制約**
- **JP production default では SF11/SF12 を無条件に許さない**
- **CAD は便利だが、JP carrier sense の唯一根拠にはしない**
- **single-channel head では airtime がシステム容量そのもの**
- **LoRa に載らない payload は summary / Wi-Fi / reject のどれかに必ず落とす**

詳細な raw 表は `calc/` を参照。
