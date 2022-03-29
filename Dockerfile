FROM golang:1.17 as builder

ENV GOPATH /go

RUN mkdir -p /app

COPY . /app

WORKDIR /app

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o tunnel

RUN chmod -R +x /app

FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

COPY --from=builder /app/tunnel /app/tunnel

EXPOSE 8080

ENTRYPOINT ["/app/tunnel"]
