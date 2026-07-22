FROM golang:1.26 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY proto ./proto
COPY services/router ./services/router
COPY services/shared ./services/shared
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/knot-router ./services/router/cmd/knot-router

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/knot-router /knot-router

ENTRYPOINT ["/knot-router"]
