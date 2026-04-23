# Platform Pinmap And Electrical Notes

## 1. この章の目的

この章では、**ESP32-S3 + Wio-SX1262 kit を実際に実装対象として見たときの使えるピン / 使いにくいピン / 予約済みピン** を整理します。

重要なのは次の2点です。

1. **XIAO side header のピン表**
2. **Wio-SX1262 が B2B / expansion 側で占有するピン**

## 2. XIAO side header の基本

Seeed の XIAO ESP32-S3 pin table から、side header 側の主要ピンは以下です。 `[S2]`

| XIAO Label | ESP32 GPIO | 主な用途 | 備考 |
|---|---:|---|---|
| 5V | VBUS | 電源 | USB 5V |
| GND | - | GND | - |
| 3V3 | 3V3_OUT | 電源 | 3.3V out |
| D0 / A0 | GPIO1 | GPIO / ADC / Touch | 汎用で使いやすい |
| D1 / A1 | GPIO2 | GPIO / ADC / Touch | 汎用で使いやすい |
| D2 / A2 | GPIO3 | GPIO / ADC / Touch | 汎用で使いやすい |
| D3 / A3 | GPIO4 | GPIO / ADC / Touch | 汎用で使いやすい |
| D4 / A4 / SDA | GPIO5 | I2C SDA / ADC / Touch | 外部センサー向き |
| D5 / A5 / SCL | GPIO6 | I2C SCL / ADC / Touch | 外部センサー向き |
| D6 / TX | GPIO43 | UART TX | GPS / debug / bridge に便利 |
| D7 / RX | GPIO44 | UART RX | GPS / debug / bridge に便利 |
| D8 / A8 / SCK | GPIO7 | SPI SCK / ADC | **LoRa SPI 共有** |
| D9 / A9 / MISO | GPIO8 | SPI MISO / ADC | **LoRa SPI 共有** |
| D10 / A10 / MOSI | GPIO9 | SPI MOSI / ADC | **LoRa SPI 共有** |

### 2.1 A11 / A12 の注意

Seeed の XIAO ESP32-S3 page では、GPIO41 / GPIO42 を A11 / A12 として言及しつつも、  
**ADC としては使えない** という注意があります。 `[S2]`

本リポジトリでは、GPIO41 / GPIO42 を「アナログ入力候補」としては扱いません。

## 3. Wio-SX1262 module 自体の信号

Wio-SX1262 module datasheet の pinout は以下です。 `[S4]`

| Module Pin | Name | 役割 |
|---:|---|---|
| 1 | RF_SW | 内部 RF switch 制御 |
| 2 | MISO | SPI MISO |
| 3 | MOSI | SPI MOSI |
| 4 | SCK | SPI SCK |
| 5 | NRST | reset, active low |
| 6 | NSS | SPI chip select |
| 7 | GND | GND |
| 8 | VCC | power |
| 9 | ANT | RF |
| 10 | GND | GND |
| 11 | BUSY | radio busy |
| 12 | DIO1 | generic IRQ |

### 3.1 module 実装で重要な信号

repo 実装で特に大事なのは次です。

- SPI 4本
- NSS
- NRST
- BUSY
- DIO1
- RF_SW

さらに datasheet では、DIO3 を TCXO 電源制御に使う説明があります。  
ただし Wio-SX1262 module ではこの内部配線は module 側で処理されており、外部 MCU から主に見るべきは BUSY / DIO1 / RF_SW / SPI / RESET です。 `[S4]`

## 4. Wio-SX1262 for XIAO 基板で実際に結線される GPIO

Wio-SX1262 for XIAO schematic と XIAO ESP32-S3 schematic を突き合わせると、  
LoRa 関連は実質以下の GPIO に結線されます。 `[S5][S6]`

| LoRa Function | ESP32 GPIO | 備考 |
|---|---:|---|
| LORA_SPI_SCK | GPIO7 | XIAO D8 |
| LORA_SPI_MISO | GPIO8 | XIAO D9 |
| LORA_SPI_MOSI | GPIO9 | XIAO D10 |
| LORA_SPI_NSS | GPIO41 | B2B / expansion 側 |
| LORA_RST | GPIO42 | B2B / expansion 側 |
| LORA_DIO1 | GPIO39 | B2B / expansion 側 |
| LORA_BUSY | GPIO40 | B2B / expansion 側 |
| LORA_RF_SW1 | GPIO38 | B2B / expansion 側 |

### 4.1 board 機能でさらに使われる GPIO

Wio-SX1262 for XIAO schematic 上、追加で以下の board function が見えます。 `[S5]`

| Board Function | ESP32 GPIO | 備考 |
|---|---:|---|
| Wio green LED | GPIO48 | board LED |
| Wio user button | GPIO21 | board button |
| extra expansion line | GPIO47 | LoRa core では未使用 |

### 4.2 XIAO board 側との衝突に注意

XIAO board page では **USER_LED = GPIO21** とされます。 `[S2]`  
一方、Wio-SX1262 for XIAO schematic では **GPIO21 に user button** がぶら下がります。 `[S5]`

したがって、repo 実装では GPIO21 を「ただの自由GPIO」とみなさず、  
**board feature shared line** として扱うべきです。

## 5. 使い分けの実務ルール

### 5.1 比較的安全に使いやすい
- D0〜D5
- D6 / D7

→ 外部センサー、I2C、UART、割り込み入力に向く

### 5.2 共有前提で使う
- D8 / D9 / D10

→ LoRa SPI と共有。  
  外部 SPI デバイスを追加する場合は、CS 分離、タイミング管理、driver ownership を明確化する。

### 5.3 原則予約扱い
- GPIO38
- GPIO39
- GPIO40
- GPIO41
- GPIO42

→ LoRa core control line。  
  repo では HAL 内で所有し、アプリから直接触らせない。

### 5.4 board feature として予約
- GPIO21
- GPIO48

→ button / LED 用途。  
  使う場合は board abstraction を通す。

## 6. 外部 1x7 header の扱い

Wio-SX1262 for XIAO 基板には J1/J2 の 1x7 header があり、schematic 上は概ね以下です。 `[S5]`

- J1: LORA_DIO1 / LORA_BUSY / LORA_RST / LORA_SPI_NSS / LORA_RF_SW1 / NC / NC
- J2: VIN / GND / 3V3 / LORA_SPI_MOSI / LORA_SPI_MISO / LORA_SPI_SCK / NC

ただし、Seeed wiki のハードウェア図では外部 header に D5/D6/D7 のような表記が見えるケースがあり、  
**公開 schematic と画像表記の差分**が残ります。 `[S1][S5]`

本仕様では次の扱いを推奨します。

- repo コードは **schematic を正本** とする
- 実機拡張をする前に **現物の導通確認** をする
- external header を core requirement に含めない

## 7. この repo での GPIO ポリシー

### 7.1 Node SDK 側
Node application に直接公開してよいのは原則として以下。

- D0〜D5
- D6 / D7

### 7.2 Radio HAL 側
Radio HAL が専有するもの。

- GPIO7 / GPIO8 / GPIO9
- GPIO38 / GPIO39 / GPIO40 / GPIO41 / GPIO42

### 7.3 Board HAL 側
Board HAL が抽象化するもの。

- GPIO21 (button/shared line)
- GPIO48 (board LED)

## 8. 実装上の注意

1. **GPIO raw number を正本にする**  
   XIAO の資料は board revision, plus/sense, expansion header 文脈で別名が揺れやすい。  
   repo 内部は raw GPIO 番号で統一する。

2. **NSS / RESET / BUSY / DIO1 / RF_SW を app に触らせない**  
   LoRa driver の state machine が壊れる。

3. **GPIO21 shared-line 問題を無視しない**  
   button と LED / user feedback の設計は board abstraction に逃がす。

4. **SPI 共有は v1 で極力避ける**  
   まずは I2C / UART で周辺を増やす。

## 9. 主な根拠

- `[S2]` XIAO side pin table
- `[S4]` Wio-SX1262 module pinout
- `[S5]` Wio-SX1262 for XIAO schematic
- `[S6]` XIAO ESP32-S3 schematic
