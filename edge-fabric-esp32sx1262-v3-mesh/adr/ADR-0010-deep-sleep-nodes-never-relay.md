# ADR-0010 Deep-sleep nodes never relay

Decision: `sleepy_leaf` / deep-sleep node は relay/backbone role を取らない。

Why:
- relay は listen cost が大きい
- deep sleep では Wi-Fi connection を維持できない
- battery life estimation を壊さないため
