# LoRa Calc Files

このディレクトリは、LoRa の airtime / payload / hop budget を検討するための補助資料です。

## Files

- `lora_toa_matrix_bw125.csv`  
  raw LoRa TOA matrix (BW125)

- `lora_toa_matrix_bw250.csv`  
  raw LoRa TOA matrix (BW250)

- `lora_profile_caps.csv`  
  v1 JP-safe profile の payload cap と app body cap

- `lora_airtime_caps.csv`  
  v1 profile の raw airtime cap まとめ

- `lora_mesh_effective_occupancy.csv`  
  relay hop 数に応じた effective channel occupancy の概算  
  direct=1送信, 1-relay=2送信, 2-relay=3送信

- `lora_mesh_body_caps.csv`  
  direct overhead 14B, relayed overhead 18B を仮定した body cap 比較

- `lora_toa_raw.py`  
  raw TOA 近似計算スクリプト

## Important note

`effective_channel_occupancy_ms` は network-wide planning 用の概算です。  
実際の成功率 / collision / backoff / ack 往復 / queue pressure は別途測る必要があります。

この repo では、
- relay を増やすほど occupancy は重くなる
- relayed payload は body cap が減る
という intuition を Codex / reviewer がすぐ確認できるように、  
この 2 つの CSV を追加しています。
