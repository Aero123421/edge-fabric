# ADR-0013 Route by message class, not by bearer API

Decision: SDK は bearer 指定 API を出さず、`route_class` と message semantics を主語にする。

Why:
- アプリを通信詳細から切り離すため
- Wi-Fi / LoRa / relay / summary codec の最適化を fabric 側で行うため
