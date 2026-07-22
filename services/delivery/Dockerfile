FROM golang:1.26 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY proto ./proto
COPY services/delivery ./services/delivery
COPY services/shared ./services/shared
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/knot-delivery ./services/delivery/cmd/knot-delivery

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/knot-delivery /knot-delivery

ENTRYPOINT ["/knot-delivery"]
