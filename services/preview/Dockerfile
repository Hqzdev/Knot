FROM golang:1.26 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY proto ./proto
COPY services/preview ./services/preview
COPY services/shared ./services/shared
RUN CGO_ENABLED=0 go build -o /out/knot-preview ./services/preview/cmd/knot-preview
RUN CGO_ENABLED=0 go build -o /out/knot-healthcheck ./services/shared/healthcheck

FROM gcr.io/distroless/static-debian12

COPY --from=build /out/knot-preview /knot-preview
COPY --from=build /out/knot-healthcheck /knot-healthcheck

USER nonroot:nonroot

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5 CMD ["/knot-healthcheck", "http://127.0.0.1:8087/healthz"]

ENTRYPOINT ["/knot-preview"]
