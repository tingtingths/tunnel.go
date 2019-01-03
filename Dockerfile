FROM golang:1.11

ENV GOPATH /go

RUN mkdir -p /app

COPY . /app

WORKDIR /app

RUN go get -d -v "github.com/t-tomalak/logrus-easy-formatter" \
    && go get -d -v "github.com/sirupsen/logrus"

RUN go build -o tunnel .

RUN chmod -R +x /app

ENTRYPOINT ["go"]
CMD ["run", "/app/tunnel.go"]
