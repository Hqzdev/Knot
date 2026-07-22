FROM golang:1.26 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY services/attachments ./services/attachments
COPY services/shared ./services/shared
RUN CGO_ENABLED=0 go build -o /out/knot-attachments ./services/attachments/cmd/knot-attachments

FROM gcr.io/distroless/static-debian12

COPY --from=build /out/knot-attachments /knot-attachments

USER nonroot:nonroot

ENTRYPOINT ["/knot-attachments"]
