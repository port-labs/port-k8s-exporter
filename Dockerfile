FROM golang:1.19-alpine

RUN apk add --no-cache wget make g++ openssl-dev autoconf automake libtool curl librdkafka-dev

USER nonroot:nonroot

ENTRYPOINT ["/usr/bin/port-k8s-exporter"]

COPY port-k8s-exporter /usr/bin/port-k8s-exporter
