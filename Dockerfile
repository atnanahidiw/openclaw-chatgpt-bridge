FROM golang:1.22-alpine AS build

WORKDIR /src

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

COPY go.mod ./
COPY main.go ./
COPY main_test.go ./

RUN go test ./...
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}" \
    -o /out/openclaw-chatgpt-bridge .

FROM alpine:3.20

RUN adduser -D -H -u 10001 appuser
WORKDIR /app
COPY --from=build /out/openclaw-chatgpt-bridge /app/openclaw-chatgpt-bridge

USER 10001
EXPOSE 8080

CMD ["/app/openclaw-chatgpt-bridge"]
