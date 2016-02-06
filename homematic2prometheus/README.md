Simple tool which registers at the [HomeMatic
CCU2](http://www.eq-3.de/produkt-detail-zentralen-und-gateways/items/homematic-zentrale-ccu-2.html)
in order to receive events (like new temperature or humidity values). These
values are then pushed to a [Prometheus push
gateway](https://github.com/prometheus/pushgateway).

Originally, I intended for this to run on the CCU2 itself, but unfortunately
the CCU2 does not support IPv6 yet, and I don’t want to make my push gateway
available via IPv4. Hence, I’m running homematic2prometheus on a Raspberry Pi.

Use `GOARCH=arm GOARM=5 go build` to cross-compile for a Raspberry Pi.

