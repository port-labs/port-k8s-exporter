FROM alpine

COPY assets/ /assets

#USER nonroot:nonroot
RUN apk add gcompat

ENTRYPOINT ["/usr/bin/port-k8s-exporter"]

COPY port-k8s-exporter /usr/bin/port-k8s-exporter
