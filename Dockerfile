FROM alpine

COPY assets/ /assets

ENTRYPOINT ["/usr/bin/port-k8s-exporter"]

COPY port-k8s-exporter /usr/bin/port-k8s-exporter
