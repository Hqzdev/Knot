FROM golang:1.26 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY services/attachments ./services/attachments
COPY services/shared ./services/shared
RUN CGO_ENABLED=0 go build -o /out/knot-attachments ./services/attachments/cmd/knot-attachments
RUN CGO_ENABLED=0 go build -o /out/knot-healthcheck ./services/shared/healthcheck

FROM gcr.io/distroless/static-debian12

COPY --from=build /out/knot-attachments /knot-attachments
COPY --from=build /out/knot-healthcheck /knot-healthcheck

USER nonroot:nonroot

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5 CMD ["/knot-healthcheck", "http://127.0.0.1:8082/readyz"]

ENTRYPOINT ["/knot-attachments"]
