FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/wellknown-overlay ./cmd/wellknown-overlay

FROM nginx:1.29-alpine

COPY --from=build /out/wellknown-overlay /usr/local/bin/wellknown-overlay
COPY docker/gateway/entrypoint.sh /usr/local/bin/wellknown-overlay-gateway
COPY docker/gateway/placeholder /usr/share/wellknown-overlay/placeholder

RUN chmod +x /usr/local/bin/wellknown-overlay-gateway

EXPOSE 80
ENTRYPOINT ["/usr/local/bin/wellknown-overlay-gateway"]
