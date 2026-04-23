# Official Source Registry

このリポジトリ内の `[Sx]` ラベルは、以下の一次情報または一次情報に近い公開資料を指します。

- **[S1]** Seeed Studio, *Wio-SX1262 With XIAO ESP32S3 Kit*  
  https://wiki.seeedstudio.com/wio_sx1262_with_xiao_esp32s3_kit/

- **[S2]** Seeed Studio, *Getting Started with Seeed Studio XIAO ESP32-S3 Series*  
  https://wiki.seeedstudio.com/xiao_esp32s3_getting_started/

- **[S3]** Espressif, *ESP32-S3 Series Datasheet*  
  https://documentation.espressif.com/esp32-s3_datasheet_en.pdf

- **[S4]** Seeed Studio, *Wio-SX1262 Module Datasheet*  
  https://files.seeedstudio.com/products/SenseCAP/Wio_SX1262/Wio-SX1262_Module_Datasheet.pdf

- **[S5]** Seeed Studio, *Schematic Diagram Wio-SX1262 for XIAO*  
  https://files.seeedstudio.com/products/SenseCAP/Wio_SX1262/Schematic_Diagram_Wio-SX1262_for_XIAO.pdf

- **[S6]** Seeed Studio, *XIAO ESP32-S3 Schematic v1.3*  
  https://files.seeedstudio.com/wiki/SeeedStudio-XIAO-ESP32S3/res/XIAO_ESP32S3_V1.3_SCH_260115.pdf

- **[S7]** Seeed Studio / TIMCO, *TELEC certificate for XIAO ESP32S3 / XIAO ESP32S3 Sense*  
  https://files.seeedstudio.com/Seeed_Certificate/documents_certificate/102010635-TELEC.pdf

- **[S8]** Seeed Studio / Kiwa, *TELEC certificate for Wio-SX1262*  
  https://files.seeedstudio.com/Seeed_Certificate/documents_certificate/113010003-TELEC.pdf

- **[S9]** Espressif, *Wi-Fi Driver / Overview (ESP32-S3)*  
  https://docs.espressif.com/projects/esp-idf/en/stable/esp32s3/api-guides/wifi-driver/overview.html

- **[S10]** Espressif, *ESP-NOW API / Config Notes*  
  https://docs.espressif.com/projects/esp-idf/en/stable/esp32s3/api-reference/network/esp_now.html

- **[S11]** Espressif, *USB Device Stack - ESP32-S3*  
  https://docs.espressif.com/projects/esp-idf/en/stable/esp32s3/api-reference/peripherals/usb_device.html

- **[S12]** Espressif, *USB OTG Console - ESP32-S3*  
  https://docs.espressif.com/projects/esp-idf/en/stable/esp32s3/api-guides/usb-otg-console.html

- **[S13]** ARIB, *標準規格概要（STD-T108）*  
  https://www.arib.or.jp/kikaku/kikaku_tushin/desc/std-t108.html

- **[S14]** IIJ, *LoRaWANの現在地とWi-Fi HaLowの展望*  
  https://www.iij.ad.jp/dev/report/iir/065/02.html

- **[S15]** Espressif, *Sleep Modes - ESP32-S3*  
  https://docs.espressif.com/projects/esp-idf/en/stable/esp32s3/api-reference/system/sleep_modes.html

- **[S16]** ARIB, *STD-T108 English PDF*  
  https://www.arib.or.jp/english/html/overview/doc/5-STD-T108v1_0-E1.pdf

- **[S17]** Semtech, *LoRa Basics™ Modem Relay: A Low-Cost Battery Powered Network Extender*  
  https://info.semtech.com/hubfs/LoRa-Basics%20Modem%20Relay%20A%20Low-Cost%20Battery%20Powered%20Network%20Extender-Whitepaper-F.pdf

- **[S18]** Semtech, *Introduction to Channel Activity Detection*  
  https://www.semtech.com/uploads/technology/LoRa/cad-ensuring-lora-packets.pdf

- **[S19]** machineQ / Comcast, *LoRaWAN Sensor Design Conversion Guide (v1.2)*  
  https://info.semtech.com/hubfs/machineq_Sensor-Design-Conversion-Guide.pdf

- **[S20]** Espressif, *ESP-WIFI-MESH Guide (Overview / Self-organized / Self-healing)*  
  https://docs.espressif.com/projects/esp-idf/en/stable/esp32s3/api-guides/esp-wifi-mesh.html

- **[S21]** Espressif, *ESP-WIFI-MESH API Reference*  
  https://docs.espressif.com/projects/esp-idf/en/stable/esp32s3/api-reference/network/esp-wifi-mesh.html

- **[S22]** Espressif, *ESP-FAQ Handbook (Mesh Q&A)*  
  https://docs.espressif.com/projects/esp-faq/en/latest/esp-faq-en-master.pdf

- **[S23]** LoRa Alliance, *LoRaWAN® Link Layer Specification v1.0.4*  
  https://lora-alliance.org/wp-content/uploads/2021/11/LoRaWAN-Link-Layer-Specification-v1.0.4.pdf

- **[S24]** LoRa Alliance, *What is LoRaWAN®?*  
  https://lora-alliance.org/about-lorawan-old/

- **[S25]** Semtech, *Gateway FAQ*  
  https://www.semtech.com/design-support/faq/faq-gateway

- **[S26]** Semtech, *SX1262 Product Page*  
  https://www.semtech.com/products/wireless-rf/lora-connect/sx1262

## 読み方

- `[S1][S4]` のように複数のラベルが並んでいる場合、同じ段落が複数の根拠に依拠しています。
- 仕様本文では、**公開資料の事実**と**この repo の設計判断**を分けています。
- public source に数値の揺れがある箇所は、差分をそのまま残し、repo 側で conservative default を選びます。
- mesh / routing / payload cap / hop limit の一部は、source の事実そのものではなく、**この repo の v1/v1.1 design decision** です。そこは ADR で固定します。
