FROM golang:1.26 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY proto ./proto
COPY services/delivery ./services/delivery
COPY services/shared ./services/shared
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/knot-delivery ./services/delivery/cmd/knot-delivery
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/knot-healthcheck ./services/shared/healthcheck

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/knot-delivery /knot-delivery
COPY --from=build /out/knot-healthcheck /knot-healthcheck

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5 CMD ["/knot-healthcheck", "http://127.0.0.1:8085/readyz"]

ENTRYPOINT ["/knot-delivery"]
