FROM alpine

COPY assets/ /assets

#USER nonroot:nonroot
RUN echo "aaa"
RUN if apk --print-arch | grep -q x86_64; then apk add gcompat; fi

ENTRYPOINT ["/usr/bin/port-k8s-exporter"]

COPY port-k8s-exporter /usr/bin/port-k8s-exporter
