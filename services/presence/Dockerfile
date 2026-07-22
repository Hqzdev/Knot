FROM golang:1.26 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY services/presence ./services/presence
COPY services/shared ./services/shared
RUN CGO_ENABLED=0 go build -o /out/knot-presence ./services/presence/cmd/knot-presence

FROM gcr.io/distroless/static-debian12

COPY --from=build /out/knot-presence /knot-presence

USER nonroot:nonroot

ENTRYPOINT ["/knot-presence"]
