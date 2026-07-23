# Pin the build stage to the machine doing the building and cross-compile from
# there. Go cross-compiles natively, so building an amd64 image on an arm64 host
# costs nothing, where emulating the whole toolchain would be many times slower.
FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS build

WORKDIR /src

ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

COPY go.mod ./
COPY *.go ./

# Tests run on the build host's own architecture, which is the point: they
# exercise the code, not the target platform.
RUN go test ./...
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-amd64} go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}" \
    -o /out/openclaw-chatgpt-bridge .

FROM alpine:3.20

# ca-certificates is required whenever OPENCLAW_WEBHOOK_URL is https, which it
# is any time OpenClaw sits behind `tailscale serve`. Without it the static Go
# binary has no trust store and every upstream call fails with
# "x509: certificate signed by unknown authority".
RUN apk add --no-cache ca-certificates && adduser -D -H -u 10001 appuser
WORKDIR /app
COPY --from=build /out/openclaw-chatgpt-bridge /app/openclaw-chatgpt-bridge

USER 10001
EXPOSE 8080

CMD ["/app/openclaw-chatgpt-bridge"]
