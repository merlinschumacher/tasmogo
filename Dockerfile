# build stage
FROM golang:alpine as builder
RUN apk update && apk add --no-cache git ca-certificates && update-ca-certificates
WORKDIR /

LABEL org.opencontainers.image.source https://github.com/merlinschumacher/tasmogo

COPY go.mod .
COPY go.sum .

RUN go mod download

COPY tasmogo.go .

RUN CGO_ENABLED=0 go build

# final stage
FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /tasmogo /
ENTRYPOINT ["/tasmogo"]