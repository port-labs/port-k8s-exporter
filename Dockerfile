FROM alpine:3.21.0

RUN apk upgrade libssl3 libcrypto3

COPY assets/ /assets

ENTRYPOINT ["/usr/bin/port-k8s-exporter"]

COPY port-k8s-exporter /usr/bin/port-k8s-exporter
