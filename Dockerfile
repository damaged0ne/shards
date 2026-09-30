FROM golang:1.25-bookworm AS backend-builder
RUN apt update && apt install -y liblz4-dev
WORKDIR /tmp/src
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY . .
ARG VERSION=unknown
RUN go build -mod=readonly -ldflags "-X main.version=$VERSION" -o shards .


FROM registry.access.redhat.com/ubi9/ubi

ARG VERSION=unknown
LABEL name="shards" \
      vendor="shards contributors" \
      maintainer="shards contributors" \
      org.opencontainers.image.source="https://github.com/damaged0ne/shards" \
      org.opencontainers.image.licenses="Apache-2.0" \
      version=${VERSION} \
      release="1" \
      summary="shards: open-source observability, alerting and incident center." \
      description="shards container image (a derivative of Coroot, Apache-2.0)."

COPY LICENSE /licenses/LICENSE
COPY NOTICE /licenses/NOTICE

COPY --from=backend-builder /tmp/src/shards /usr/bin/shards
RUN mkdir /data && chown 65534:65534 /data

USER 65534:65534
VOLUME /data
EXPOSE 8080

ENTRYPOINT ["/usr/bin/shards"]
