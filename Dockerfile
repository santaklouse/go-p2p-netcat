# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e

FROM --platform=$BUILDPLATFORM golang:1.25.7-alpine3.23@sha256:f6751d823c26342f9506c03797d2527668d095b0a15f1862cddb4d927a7a4ced AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" \
    go build \
      -buildvcs=false \
      -trimpath \
      -ldflags="-s -w -X github.com/santaklouse/go-p2p-netcat/internal/cli.Version=${VERSION}" \
      -o /out/p2p-nc \
      ./cmd/p2p-nc

FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

LABEL org.opencontainers.image.source="https://github.com/santaklouse/go-p2p-netcat"
LABEL org.opencontainers.image.description="PeerId-addressed netcat with TCP and UDP forwarding"
LABEL org.opencontainers.image.licenses="MIT"

RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 65532 p2p-netcat \
    && adduser -S -D -H -h /config -u 65532 -G p2p-netcat p2p-netcat \
    && install -d -m 0700 -o p2p-netcat -g p2p-netcat /config/p2p-netcat

COPY --from=build /out/p2p-nc /usr/local/bin/p2p-nc
COPY LICENSE /usr/share/licenses/p2p-netcat/LICENSE

RUN ln -s /usr/local/bin/p2p-nc /usr/local/bin/pnc \
    && ln -s /usr/local/bin/p2p-nc /usr/local/bin/p2p-netcat

ENV HOME=/config
ENV XDG_CONFIG_HOME=/config

VOLUME ["/config"]
EXPOSE 4001/tcp
EXPOSE 4001/udp

USER 65532:65532
STOPSIGNAL SIGTERM

ENTRYPOINT ["/usr/local/bin/p2p-nc"]
CMD ["--help"]
