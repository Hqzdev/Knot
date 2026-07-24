FROM golang:1.26 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY services/api ./services/api
COPY services/shared ./services/shared
RUN CGO_ENABLED=0 go build -o /out/knot-api ./services/api/cmd/knot-api
RUN CGO_ENABLED=0 go build -o /out/knot-healthcheck ./services/shared/healthcheck

FROM gcr.io/distroless/static-debian12

COPY --from=build /out/knot-api /knot-api
COPY --from=build /out/knot-healthcheck /knot-healthcheck

USER nonroot:nonroot

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5 CMD ["/knot-healthcheck", "http://127.0.0.1:8080/ready"]

ENTRYPOINT ["/knot-api"]
