FROM golang:1.26 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY proto ./proto
COPY services/bot ./services/bot
COPY services/shared ./services/shared
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/knot-bot ./services/bot/cmd/knot-bot
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/knot-healthcheck ./services/shared/healthcheck

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/knot-bot /knot-bot
COPY --from=build /out/knot-healthcheck /knot-healthcheck

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5 CMD ["/knot-healthcheck", "http://127.0.0.1:8088/ready"]

ENTRYPOINT ["/knot-bot"]
