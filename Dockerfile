# Easee OCPP Proxy — multi-stage build for Docker / ZimaOS.
#
# Build (on the NAS or any host with Docker):
#   docker build -t easee-ocpp-proxy:local .
#
# The resulting image is small (~30 MB) and runs the proxy as a
# non-root user. Timezone data is embedded in the binary, so no
# tzdata package is needed.

FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/easee-proxy ./cmd/proxy

FROM alpine:3.20
RUN apk add --no-cache ca-certificates \
 && addgroup -S proxy \
 && adduser -S -G proxy proxy
COPY --from=build /out/easee-proxy /usr/local/bin/easee-proxy
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
COPY config.example.yaml /usr/local/share/easee-proxy/config.example.yaml
RUN chmod +x /usr/local/bin/docker-entrypoint.sh
VOLUME ["/config"]
EXPOSE 9000
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
