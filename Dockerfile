FROM golang:1.14 as builder

ENV GOPATH /go

RUN mkdir -p /app

COPY . /app

WORKDIR /app

RUN go build -o tunnel .

RUN chmod -R +x /app

FROM alpine:latest

COPY --from=builder /app/tunnel /app/tunnel

EXPOSE 50080

ENTRYPOINT ["/app/tunnel"]