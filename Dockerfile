# Ozon rFBS — серверная версия для OpenMediaVault / Docker.
# Сборка: docker build -t rfbs_server .

FROM golang:1.22-alpine AS build
WORKDIR /src
COPY app/ ./
COPY SERVER_VERSION /SERVER_VERSION
ARG GIT_SHA=local
RUN go vet ./... \
 && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
      -ldflags "-s -w -X main.appVersion=$(tr -d ' \r\n' < VERSION) -X main.serverVersion=$(tr -d ' \r\n' < /SERVER_VERSION)-$(echo ${GIT_SHA} | cut -c1-7)" \
      -o /out/rfbs .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata su-exec wget
COPY --from=build /out/rfbs /usr/local/bin/rfbs
COPY docker/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENV OZON_RFBS_ADDR=0.0.0.0:8766 \
    OZON_RFBS_DATA=/data \
    TZ=Europe/Moscow \
    PUID=1000 \
    PGID=100

VOLUME /data
EXPOSE 8766
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8766/api/ping || exit 1
ENTRYPOINT ["/entrypoint.sh"]
