FROM golang:1.21-alpine

USER nonroot:nonroot

ENTRYPOINT ["/usr/bin/port-k8s-exporter"]

COPY port-k8s-exporter /usr/bin/port-k8s-exporter
