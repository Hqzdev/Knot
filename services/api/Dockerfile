FROM golang:1.26 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY services/api ./services/api
COPY services/shared ./services/shared
RUN CGO_ENABLED=0 go build -o /out/knot-api ./services/api/cmd/knot-api

FROM gcr.io/distroless/static-debian12

COPY --from=build /out/knot-api /knot-api

USER nonroot:nonroot

ENTRYPOINT ["/knot-api"]
