FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/wellknown-overlay ./cmd/wellknown-overlay

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/wellknown-overlay /usr/local/bin/wellknown-overlay

EXPOSE 8765
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/wellknown-overlay"]
CMD ["serve", "-config", "/etc/wellknown-overlay/overlay.json", "-root", "/var/lib/wellknown-overlay/public", "-listen", ":8765"]

