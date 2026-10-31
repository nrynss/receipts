FROM golang:1.27.1-alpine AS build
RUN apk add --no-cache nodejs npm
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN cd web && npm ci && npm run build
RUN mkdir -p cmd/receipts/static && cp -R web/build/. cmd/receipts/static/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/receipts ./cmd/receipts

FROM alpine:3.22
RUN apk add --no-cache ca-certificates ffmpeg streamlink \
    && addgroup -S receipts && adduser -S -G receipts -u 10001 receipts \
    && mkdir -p /data && chown receipts:receipts /data
COPY --from=build /out/receipts /usr/local/bin/receipts
ENV RECEIPTS_MODE=live RECEIPTS_AUTH_ENABLED=true RECEIPTS_HTTP_ADDRESS=:8080 \
    RECEIPTS_DATABASE_PATH=/data/receipts.db RECEIPTS_TRANSCRIPT_RETENTION=30m \
    RECEIPTS_MAX_ACTIVE_CHANNELS=1
USER 10001:10001
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=20s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/receipts"]
