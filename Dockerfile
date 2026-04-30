# iosuite — single-binary container image.
#
# Multi-stage: stage 1 builds the Go binary, stage 2 ships it on a
# minimal base. CGO_ENABLED=0 means we get a fully static binary
# that runs on alpine without libc concerns. Final image is a few
# tens of MB — most of that is the binary itself, not OS layers.
#
# Built and pushed by CI to ghcr.io/ls-ads/iosuite:<tag>; consumers
# (iosuite.io's docker-compose.dev.yml, vps-config-uat's apps role)
# pull from there.

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY cmd/      ./cmd/
COPY internal/ ./internal/
ARG VERSION=dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 go build \
        -trimpath \
        -ldflags "-s -w \
            -X iosuite.io/internal/version.Version=${VERSION} \
            -X iosuite.io/internal/version.Commit=${COMMIT}" \
        -o /out/iosuite ./cmd/iosuite

FROM alpine:3.20
# ca-certificates so Go's HTTPS client can verify the RunPod API
# (api.runpod.ai serves a Let's Encrypt cert; without ca-certs the
# RunPodProvider's /health probe at startup fails with x509: cannot
# load system roots).
RUN apk add --no-cache ca-certificates && update-ca-certificates
COPY --from=build /out/iosuite /usr/local/bin/iosuite

# `iosuite serve --provider runpod` is the production entrypoint;
# explicit args at run-time override.
ENTRYPOINT ["iosuite"]
CMD ["--help"]
