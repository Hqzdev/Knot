FROM golang:1.26 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY proto ./proto
COPY services/gateway ./services/gateway
COPY services/shared ./services/shared
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/knot-gateway ./services/gateway/cmd/knot-gateway

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/knot-gateway /knot-gateway

ENTRYPOINT ["/knot-gateway"]
