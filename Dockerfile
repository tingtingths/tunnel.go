FROM golang:1.11

ENV GOPATH /go

RUN mkdir -p /app

COPY src /app

WORKDIR /app

RUN go get "github.com/t-tomalak/logrus-easy-formatter" \
    && go get "github.com/sirupsen/logrus"

RUN go build -o tunnel .

RUN chmod -R +x /app

CMD ["/app/tunnel"]
