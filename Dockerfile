FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /build
COPY go.mod ./
COPY . .

RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /chameleon ./cmd/chameleon/

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /chameleon /chameleon

EXPOSE 1080 10080

ENTRYPOINT ["/chameleon"]
