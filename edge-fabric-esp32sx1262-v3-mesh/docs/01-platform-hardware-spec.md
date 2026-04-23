# Platform Hardware Spec

## 1. 対象ハード

この仕様で前提とする最小ハードは以下です。

- **MCU Board**: Seeed Studio XIAO ESP32-S3 `[S2]`
- **LoRa Add-on**: Wio-SX1262 for XIAO `[S1][S5]`
- **組み合わせ**: XIAO ESP32S3 & Wio-SX1262 Kit `[S1]`

この組み合わせは、**ESP32-S3 側で Wi-Fi / BLE / USB / local processing** を行い、  
**SX1262 側で sub-GHz LoRa** を扱う構成です。

## 2. XIAO ESP32-S3 側の整理

### 2.1 SoC レベル

ESP32-S3 は、Espressif 公式資料では以下の特徴を持ちます。 `[S3]`

- Xtensa dual-core 32-bit LX7
- 最大 240 MHz
- 2.4 GHz Wi-Fi (802.11b/g/n)
- Bluetooth 5 (LE) / Bluetooth Mesh
- 45 programmable GPIOs
- USB OTG / USB Serial-JTAG
- UART / I2C / I2S / SPI / ADC / touch などの豊富な周辺回路

### 2.2 XIAO Board レベル

Seeed の XIAO ESP32-S3 ボードとして見ると、主に以下が重要です。 `[S2]`

- ESP32-S3R8
- 8 MB PSRAM
- 8 MB Flash
- 21 x 17.8 mm
- XIAO standard side header
- B2B / expansion header
- Type-C 5V input
- Li battery input / charge management
- User LED / Charge LED / Reset / Boot

### 2.3 電力・低消費の意味

Seeed の XIAO ESP32-S3 ページでは、typical 値として以下が示されています。 `[S2]`

- deep sleep: 14 µA class
- Wi-Fi active: 約 100 mA class
- battery charge current: board variant / revision により 50 mA と 100 mA 表記差あり

このため、**「Wi-Fi を常時張りっぱなしの battery node」より、  
「sleep 中心 + 短時間 Wi-Fi or LoRa 起動」設計** が圧倒的に相性が良いです。

## 3. Wio-SX1262 側の整理

### 3.1 モジュールの基本

Wio-SX1262 は Seeed の純RFモジュールで、主な特徴は以下です。 `[S4]`

- SX1262 ベース
- LoRa と (G)FSK をサポート
- LoRa mode 帯域幅 7.8〜500 kHz
- 通信制御は SPI
- RF port は default IPEX
- high precision active TCXO
- DC-DC design

### 3.2 公式 generic specs

Wio-SX1262 module datasheet の generic specs は次の通りです。 `[S4]`

- size: 11.6 x 11 x 2.95 mm
- supply: 3.3V typical
- sleep current: 1.62 µA
- receiver current: 7.6 mA @ BW125kHz
- transmitter current: 125 mA @ 22 dBm
- output power: max 22 dBm
- sensitivity: -136.73 dBm @ SF12 / BW125kHz
- frequency range (generic): 862–930 MHz

### 3.3 generic spec と日本運用 spec は分けて扱う

ここは極めて重要です。

- **generic module datasheet** では 862–930 MHz / max 22 dBm `[S4]`
- **日本向け TELEC certificate** では 920.6–928.0 MHz (125 kHz) と 920.7–927.9 MHz (250 kHz)、最大 10.000 mW rated `[S8]`

したがって、本番寄り用途で日本市場・日本現場を前提にするなら、  
**generic datasheet の 22 dBm を運用値として採用してはいけません。**  
このリポジトリでは日本運用時の region policy を別章で固定します。

## 4. Kit レベルで重要なこと

Seeed の kit page から見える kit-level 事情は以下です。 `[S1]`

- XIAO ESP32-S3 + Wio-SX1262 の組み合わせ
- single-channel LoRaWAN / LoRa starter として案内
- Wi-Fi / BLE / LoRa を 1 台で扱える
- Type-C 5V input
- BAT input 4.2V 表記
- charge current 100mA 表記
- Wio 側 user button、XIAO 側 user LED / charge LED
- 「Original FPC antenna is testing only」との注意

### 4.1 ここから導く実装上の意味

1. **LoRa 側は single-radio / single-channel 前提**  
   本リポジトリは「LoRaWAN concentrator を模倣する」より、  
   **custom star fabric** を作る方が自然です。

2. **I/O 余力は無限ではない**  
   使いやすい side-header はそこまで多くなく、SPI も LoRa に食われます。  
   追加センサーは I2C / UART 優先が無難です。

3. **Wi-Fi と LoRa を両方使えるが、役割分離が必要**  
   このハードは「毎パケット自動最適化」より、  
   **LoRa = sparse / critical、Wi-Fi = detailed / control / bulk** の設計と相性が良いです。

## 5. このハードに向くユースケース

### 5.1 強く向く
- battery sensor node
- alert node
- long-range sparse telemetry
- USB-connected gateway head
- site bridge / backhaul helper
- field commissioning node
- maintenance / diagnostics node

### 5.2 向くが注意が必要
- local Wi-Fi leaf
- dual-stack bridge
- display comm node
- multiple host ingress の gateway

### 5.3 向かない
- full LoRaWAN multi-channel gateway
- high-throughput streaming node
- video / audio transport node
- 大容量ログや OTA を LoRa fallback させる設計

## 6. 公式資料の表記差・要確認ポイント

このリポジトリでは、以下を**表記差あり**として扱います。

### 6.1 BAT input
- kit page: BAT input 4.2V `[S1]`
- XIAO board page: battery input / charge docs は 3.7V 公称で語られることが多い `[S2]`

→ 実務上は「**1-cell Li-ion/LiPo 系**」として扱い、  
  充放電・保護・満充電 4.2V を前提に設計する。

### 6.2 charge current
- kit page: 100mA `[S1]`
- XIAO page: board variant によって 50mA / 100mA 表記差が見える `[S2]`

→ 現物リビジョン確認と実測を推奨。  
  設計時は「**board-integrated charging is convenience path**」と見なし、  
  大容量 battery の高速充電は別設計で考える。

### 6.3 temperature
- kit page: -40°C ～ 65°C 表記 `[S1]`
- Wio module datasheet: module working temperature -40〜85°C `[S4]`
- XIAO board page: board variant で別表記がある `[S2]`

→ システム全体の連続動作温度は、**最弱部品 + battery + enclosure** で決まる。  
  実装仕様では 0〜50°C あるいは -10〜55°C など、**製品ごとの実測温度範囲**を別途決めるべき。

## 7. Repository への落とし込み

このハード事情から、本リポジトリでは次を前提にします。

- firmware は **ESP-IDF first**
- LoRa bearer は **raw LoRa / custom MAC first**
- Wi-Fi bearer は **standard Wi-Fi/IP first**
- gateway head は **USB CDC first**
- host / site router は **Linux first**
- JP region policy は **最初から組み込む**

## 8. 主な根拠

- `[S1]` kit page
- `[S2]` XIAO ESP32-S3 getting started
- `[S3]` ESP32-S3 datasheet
- `[S4]` Wio-SX1262 datasheet
