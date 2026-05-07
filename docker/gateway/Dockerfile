FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/wellknown-overlay ./cmd/wellknown-overlay
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/wellknown-overlay-docker-entrypoint ./cmd/wellknown-overlay-docker-entrypoint

FROM nginx:1.29-alpine

COPY --from=build /out/wellknown-overlay /usr/local/bin/wellknown-overlay
COPY --from=build /out/wellknown-overlay-docker-entrypoint /usr/local/bin/wellknown-overlay-docker-entrypoint
COPY docker/gateway/entrypoint.sh /usr/local/bin/wellknown-overlay-gateway
COPY docker/gateway/placeholder /usr/share/wellknown-overlay/placeholder

RUN chmod +x /usr/local/bin/wellknown-overlay-gateway

EXPOSE 80
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 CMD ["/usr/local/bin/wellknown-overlay", "healthcheck", "-url", "http://127.0.0.1/healthz"]
ENTRYPOINT ["/usr/local/bin/wellknown-overlay-gateway"]
