# Requirements Addendum

この addendum は、前回版までの requirements に対して、**mesh-first 化で増えた条件**を追加するものです。

## ADD-001
powered always-on node を backbone の前提にすること。

## ADD-002
sleepy/deep sleep node を relay に使わないこと。

## ADD-003
Wi-Fi mesh を主骨格、LoRa を sparse overlay とすること。

## ADD-004
LoRa multi-hop は “あれば何でも使う” ではなく、hop cap と relay role によって制御すること。

## ADD-005
LoRa relay を使う message は summary codec を持つこと。

## ADD-006
ESP-WIFI-MESH no-router multiple root 直接通信を前提にしないこと。 `[S22]`

## ADD-007
multi-domain site は Fabric Spine で統合すること。

## ADD-008
payload budget は “1 hop の cap” だけではなく、“relay を通した effective occupancy” まで見ること。

## ADD-009
control / servo / safety loop は local control island に寄せること。

## ADD-010
route selection は `route_class + power_class + wake_class + payload_fit + hysteresis` で決めること。

## ADD-011
sleepy node は last-good parent list を保存し、aggressive discovery をしないこと。

## ADD-012
maintenance_awake を正式な mode として持ち、通常運用と分離すること。

## ADD-013
controller client は複数あってよいが、durable writer は 1 つとすること。

## ADD-014
mesh root / gateway / relay の health を fabric summary に集約すること。

## ADD-015
LoRa path failure では alternate relay, direct fallback, queue/defer, local-only の順序で degraded handling を定義すること。
