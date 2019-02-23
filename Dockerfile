FROM golang:1.11-alpine AS builder
# need libraries for go mod (default go module is off)
RUN apk update && apk add --no-cache git gcc musl-dev
ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64
WORKDIR /build
COPY . .
RUN go install -v

FROM alpine
RUN apk update && apk add --no-cache ca-certificates
USER mogura
COPY --from=builder /go/bin/mogura /
CMD ["/mogura"]
