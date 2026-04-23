# ADR-0012 Multi-domain sites are bridged by Fabric Spine

Decision: 複数 Wi-Fi mesh domain / root を 1 site に持てるが、domain 間通信は Fabric Spine / Site Router / bridge で束ねる。

Why:
- ESP-WIFI-MESH no-router multiple roots の直接相互通信を前提にできない
- server / controller / gateway 統合を上位で扱う方が汎用的
