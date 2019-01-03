package src

import (
	"bufio"
	"crypto/tls"
	"errors"
	log "github.com/sirupsen/logrus"
	"github.com/t-tomalak/logrus-easy-formatter"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
)

type HandlerResult struct {
	conn        net.Conn
	destination string
	err         error
}

type PipeError struct {
	src  net.Conn
	dest net.Conn
	err  error
}

type PendingPipe struct {
	conn        net.Conn
	destination string
}

type Pipe struct {
	src  net.Conn
	dest net.Conn
	dir  int
	buf  []byte
}

const (
	SEND = 1
	RECV = 2
)

const readerBufSize = 64000
const chanBufSize = 5
const logLevel = log.InfoLevel
const addr = ":50080"
var cert = ""
var privKey = ""

var handlerOutboundQ = make(chan HandlerResult, chanBufSize)
var handlerInboundQ = make(chan net.Conn, chanBufSize)
var pipeErrorQ = make(chan PipeError, chanBufSize)
var pendingQ = make(chan PendingPipe, chanBufSize)

func dispatchRequest() {
	for {
		conn := <-handlerInboundQ

		log.Infof("Handling request from [%s]\n", conn.RemoteAddr().String())

		// parse HTTP request
		req, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			handlerOutboundQ <- HandlerResult{conn, "", err}
			continue
		}

		if logLevel == log.DebugLevel {
			rawBytes, dumpErr := httputil.DumpRequest(req, false)
			if dumpErr == nil {
				log.Debugln(string(rawBytes))
			}
		}

		if strings.ToLower(req.Method) != "connect" {
			_, _ = conn.Write([]byte("Unsupported request method " + req.Method + "..."))
			handlerOutboundQ <- HandlerResult{conn, "", errors.New("unsupported request method " + req.Method)}
			continue
		}

		// get target
		reqUrl := req.RequestURI
		if len(strings.TrimSpace(reqUrl)) == 0 {
			_, _ = conn.Write([]byte("Empty request URL..."))
			handlerOutboundQ <- HandlerResult{conn, "", errors.New("empty request url")}
			continue
		}

		handlerOutboundQ <- HandlerResult{conn, reqUrl, nil}
	}
}

func setupPipe() {
	for {
		p := <-pendingQ

		dest, err := net.Dial("tcp", p.destination)
		if err != nil {
			handlerOutboundQ <- HandlerResult{p.conn, p.destination, err}
			continue
		}

		log.Infof("Piped [%s] <--> [%s]", p.conn.RemoteAddr(), p.destination)

		// response OK
		_, err = p.conn.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))
		if err != nil {
			handlerOutboundQ <- HandlerResult{p.conn, p.destination, err}
			continue
		}

		go copyStream(p.conn, dest, SEND)
		go copyStream(dest, p.conn, RECV)
	}
}

func reportPipeError(src, dest net.Conn, err error) {
	pipeErrorQ <- PipeError{src, dest, err}
}

func copyStream(src, dest net.Conn, dir int) {
	buf := make([]byte, readerBufSize)
	for {
		n, err := src.Read(buf)
		if err != nil {
			reportPipeError(src, dest, err)
			break
		}

		_, err = dest.Write(buf[:n])
		if err != nil {
			reportPipeError(src, dest, err)
			break
		}

		if logLevel == log.DebugLevel {
			if dir == SEND {
				log.Debugf("(%s) --%d bytes--> (%s)\n", src.RemoteAddr(), n, dest.RemoteAddr())
			}
			if dir == RECV {
				log.Debugf("(%s) <--%d bytes-- (%s)\n", dest.RemoteAddr(), n, src.RemoteAddr())
			}
		}
	}
}

func processDispatched() {
	for {
		result := <-handlerOutboundQ
		if result.err != nil {
			log.Errorf("Fail to handle [%s], %s", result.conn.RemoteAddr(), result.err)
			_, _ = result.conn.Write([]byte("HTTP/1.1 400 " + result.err.Error()))
			_ = result.conn.Close()
			continue
		}

		pendingQ <- PendingPipe{result.conn, result.destination}
	}
}

func processPipeError() {
	for {
		e := <-pipeErrorQ
		log.Errorf("Fail to handle [%s], %s", e.src.RemoteAddr(), e.err)
		_ = e.src.Close()
		_ = e.dest.Close()
	}
}

func initListener(cert string, privKey string) (net.Listener, error) {
	if cert != "" && privKey != "" {
		// load key pair
		cert, err := tls.LoadX509KeyPair(cert, privKey)
		if err != nil {
			return nil, err
		}
		config := &tls.Config{Certificates: []tls.Certificate{cert}}
		return tls.Listen("tcp", addr, config)
	} else {
		return net.Listen("tcp", addr)
	}
}

func initLogger(level log.Level) {
	log.SetLevel(level)
	log.SetFormatter(&easy.Formatter{
		TimestampFormat: "2006-01-02 15:04:05",
		LogFormat: "[%lvl%] %time% - %msg%",
	})
}

func main() {
	if cert == "" {
		cert = os.Getenv("TUNNEL_CERT")
	}
	if privKey == "" {
		privKey = os.Getenv("TUNNEL_KEY")
	}

	initLogger(logLevel)
	l, err := initListener(cert, privKey)
	if err != nil {
		log.Fatal(err)
	}

	go processDispatched()
	go processPipeError()

	for i := 0; i < 4; i++ {
		go dispatchRequest()
		go setupPipe()
	}

	log.Infof("Listening %s...\n", addr)
	defer l.Close()

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Println(err)
			continue
		}
		handlerInboundQ <- conn
	}
}
