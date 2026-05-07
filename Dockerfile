FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN mkdir -p /out/etc/wellknown-overlay /out/var/lib/wellknown-overlay/public
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/wellknown-overlay ./cmd/wellknown-overlay
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/wellknown-overlay-docker-entrypoint ./cmd/wellknown-overlay-docker-entrypoint

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/wellknown-overlay /usr/local/bin/wellknown-overlay
COPY --from=build /out/wellknown-overlay-docker-entrypoint /usr/local/bin/wellknown-overlay-docker-entrypoint
COPY --from=build --chown=65532:65532 /out/etc/wellknown-overlay /etc/wellknown-overlay
COPY --from=build --chown=65532:65532 /out/var/lib/wellknown-overlay /var/lib/wellknown-overlay

EXPOSE 8765
USER nonroot:nonroot
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 CMD ["/usr/local/bin/wellknown-overlay", "healthcheck", "-url", "http://127.0.0.1:8765/healthz"]
ENTRYPOINT ["/usr/local/bin/wellknown-overlay-docker-entrypoint"]
