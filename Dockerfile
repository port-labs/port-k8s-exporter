FROM gcr.io/distroless/static-debian11

COPY assets/ /usr/bin/assets

USER nonroot:nonroot

ENTRYPOINT ["/usr/bin/port-k8s-exporter"]

COPY port-k8s-exporter /usr/bin/port-k8s-exporter
