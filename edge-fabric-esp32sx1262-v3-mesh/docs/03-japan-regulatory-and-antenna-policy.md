# Japan Regulatory And Antenna Policy

## 1. この章が重要な理由

SX1262 系の資料を読むと、しばしば

- 862–930 MHz
- max 22 dBm
- long range
- 915 / 868 / 923 など複数地域

という**汎用的な module spec**が前面に出ます。 `[S4]`  
しかし、日本で本番寄りに使う場合は、**実際に市場投入される kit / module の認証条件**を基準にしないと危険です。 `[S7][S8][S13]`

このリポジトリでは、日本運用を想定するビルドに対して、  
**RegionPolicy::JP を必須**とします。

## 2. Wio-SX1262 の日本向け認証条件

Wio-SX1262 の日本向け certificate では、主に以下が示されています。 `[S8]`

### 2.1 LoRa 125 kHz
- operating frequency range: **920.6–928.0 MHz**
- 38 channels
- maximum output power: **10.000 mW rated**

### 2.2 LoRa 250 kHz
- operating frequency range: **920.7–927.9 MHz**
- 37 channels
- maximum output power: **10.000 mW rated**

### 2.3 アンテナ条件
- certificate remarks: **3 antennas, max gain of 8 dBi**

## 3. XIAO ESP32-S3 の 2.4 GHz 認証条件

XIAO ESP32S3 の日本向け certificate では、2.4GHz band の unlicensed device として扱われ、  
アンテナ条件として以下が記載されています。 `[S7]`

- FPC Antenna: max gain 2.90 dBi for 2.4GHz band
- 2.4G Small Rod Antenna: max gain 2.81 dBi for 2.4GHz band

このため、2.4GHz 側も「何でも好きなアンテナを付けてよい」わけではありません。

## 4. ARIB STD-T108 の位置づけ

ARIB の標準規格概要では、STD-T108 は  
**920MHz帯テレメータ用、テレコントロール用及びデータ伝送用無線設備**を対象とする規格です。 `[S13]`

また、IIJ の解説では、日本の 920MHz 帯 LoRaWAN 運用では、  
**自分が電波を発する前に、その周波数チャネルが他システムに使われていないことを確認する**  
という運用が必要であると説明されています。 `[S14]`

この repo では、raw LoRa custom protocol であっても、**JP policy では LBT / carrier-sense 相当の設計を前提**にします。

## 5. ここから導く必須ルール

### REG-JP-001
日本向け production build は、LoRa radio region を **JP 固定**で持つこと。

### REG-JP-002
日本向け production build では、generic datasheet の `862–930 MHz / 22 dBm` を  
運用設定として選べないこと。

### REG-JP-003
LoRa のチャネル表は JP 用に別定義すること。  
最低でも certificate 上の 125 kHz / 250 kHz 範囲を外さないこと。

### REG-JP-004
送信前チャネル使用確認（LBT / carrier sense 相当）を実装すること。

### REG-JP-005
LoRa の output ceiling は JP policy 側で管理し、アプリ層から自由変更させないこと。

### REG-JP-006
アンテナ選定は certificate の gain 条件を超えないこと。  
hardware variant ごとに認証前提が変わる場合は別 profile を定義すること。

### REG-JP-007
開発用 debug build と production build を分けること。  
debug build で region override を許す場合でも、production build では封じること。

## 6. repository 設計への影響

### 6.1 Region policy を plugin ではなく core に置く
「あとで region を追加する」はよいが、  
**JP region policy 自体は v1 から core に入れる**べきです。

### 6.2 LoRa path の用途を絞る
日本では 920MHz 帯で他システムとの共存も重要であるため、  
single-channel gateway head で大量・高頻度・大容量 traffic を本線にするのは筋が悪いです。  
repo の標準方針は次とします。

- LoRa: sparse, critical, battery, fallback summary
- Wi-Fi: detailed, command, bulk, OTA

### 6.3 field UI に raw frequency を出し過ぎない
operator 画面では channel number や raw frequency を普段の主語にしない。  
ただし diagnostics では見えるようにする。

## 7. Antenna policy

### 7.1 LoRa antenna
- certificate の antenna gain 条件を守る
- 「付属アンテナ = いつでも本番使用OK」とは見なさない
- site ごとに antenna part number を inventory 化する

### 7.2 Wi-Fi antenna
- XIAO certificate の条件を超える antenna を量産系で混在させない
- enclosure 内蔵時は detuning を評価する

### 7.3 repo requirement
`hardware profile` には少なくとも次を持たせる。

- radio_region
- lora_antenna_sku
- lora_antenna_gain_dbi
- wifi_antenna_sku
- wifi_antenna_gain_dbi
- certification_profile_id

## 8. JP policy を破ると何が起きるか

- 認証前提を外す
- 現場や顧客への説明責任が破綻する
- 電波トラブル時の切り分けが困難になる
- generic sample code が現場条件に適合しない

## 9. この repo の立場

この repo は法的助言を提供するものではありません。  
ただし、**実装側が generic sample をそのまま日本本番に持ち込まないための guardrail** は必須です。  
そのため、JP policy を仕様として固定します。

## 10. 主な根拠

- `[S4]` Wio generic module datasheet
- `[S7]` XIAO ESP32S3 TELEC
- `[S8]` Wio-SX1262 TELEC
- `[S13]` ARIB STD-T108 overview
- `[S14]` IIJ explanation of carrier sense / LBT in Japan
