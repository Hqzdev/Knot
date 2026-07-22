FROM golang:1.26 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY services/push ./services/push
COPY services/shared ./services/shared
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/knot-push ./services/push/cmd/knot-push

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/knot-push /knot-push

ENTRYPOINT ["/knot-push"]
