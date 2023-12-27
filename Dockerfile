FROM golang:1.19-alpine

ENV LIBRDKAFKA_VERSION 2.3.0

RUN apk add --no-cache wget make g++ openssl-dev autoconf automake libtool curl librdkafka-dev

USER nonroot:nonroot

ENTRYPOINT ["/usr/bin/port-k8s-exporter"]

COPY port-k8s-exporter /usr/bin/port-k8s-exporter
