package main

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
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
	status      int
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

const chanBufSize = 5

// init from arguments
var addr string
var b64Cred string
var readerBufSize int
var logLevel log.Level
var methods = make(map[string]bool)

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
			handlerOutboundQ <- HandlerResult{conn, 400, "", err}
			continue
		}

		if logLevel == log.DebugLevel {
			rawBytes, dumpErr := httputil.DumpRequest(req, false)
			if dumpErr == nil {
				log.Debugln(string(rawBytes))
			}
		}

		if !methods[strings.ToLower(req.Method)] {
			handlerOutboundQ <- HandlerResult{conn, 405, "", errors.New("Unsupported method " + req.Method)}
			continue
		}

		// check credential
		if b64Cred != "" {
			var auth = req.Header.Get("Proxy-Authorization")
			auth = strings.Replace(auth, "Basic ", "", 1)
			if err != nil || auth != b64Cred {
				handlerOutboundQ <- HandlerResult{conn, 407, "", errors.New("Proxy Authentication Required")}
				continue
			}
		}

		// get target
		reqUrl := req.RequestURI
		if len(strings.TrimSpace(reqUrl)) == 0 {
			handlerOutboundQ <- HandlerResult{conn, 400, "", errors.New("Empty request url")}
			continue
		}

		handlerOutboundQ <- HandlerResult{conn, 200, reqUrl, nil}
	}
}

func setupPipe() {
	for {
		p := <-pendingQ

		dest, err := net.Dial("tcp", p.destination)
		if err != nil {
			handlerOutboundQ <- HandlerResult{p.conn, 500, p.destination, err}
			continue
		}

		log.Infof("Piped [%s] <--> [%s]\n", p.conn.RemoteAddr(), p.destination)

		// response OK
		_, err = p.conn.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))
		if err != nil {
			handlerOutboundQ <- HandlerResult{p.conn, 500, p.destination, err}
			continue
		}

		go copyStream(p.conn, dest, SEND)
		go copyStream(dest, p.conn, RECV)

		//go copyStream2(p.conn, dest)
		//go copyStream2(dest, p.conn)
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
				log.Debugf("[%s]\t--%d bytes-->\t[%s]\n", src.RemoteAddr(), n, dest.RemoteAddr())
			}
			if dir == RECV {
				log.Debugf("[%s]\t<--%d bytes--\t[%s]\n", dest.RemoteAddr(), n, src.RemoteAddr())
			}
		}
	}
}

func copyStream2(src, dest net.Conn) {
	r := bufio.NewReaderSize(src, readerBufSize)
	w := bufio.NewWriterSize(dest, readerBufSize)

	_, err := r.WriteTo(w)
	if err != nil {
		reportPipeError(src, dest, err)
	}
}

func processDispatched() {
	for {
		result := <-handlerOutboundQ
		if result.err != nil {
			log.Errorf("Fail to handle [%s], %s\n", result.conn.RemoteAddr(), result.err)
			_, _ = result.conn.Write([]byte(fmt.Sprintf("HTTP/1.1 %d %s\r\n", result.status, result.err.Error())))
			_ = result.conn.Close()
			continue
		}

		pendingQ <- PendingPipe{result.conn, result.destination}
	}
}

func processPipeError() {
	for {
		e := <-pipeErrorQ
		log.Warnf("Fail to handle [%s], %s\n", e.src.RemoteAddr(), e.err)
		_ = e.src.Close()
		_ = e.dest.Close()
	}
}

func initListener(cert string, privKey string) (net.Listener, error) {
	if cert != "" && privKey != "" {
		log.Infof("Cert: %s\n", cert)
		log.Infof("Key: %s\n", privKey)
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
		LogFormat:       "[%lvl%] %time% - %msg%",
	})
}

func parseMethodsStr(methods string) []string {
	var ret []string

	for _, s := range strings.Split(methods, ",") {
		s = strings.Trim(s, " ")
		if len(s) == 0 {
			continue
		}
		ret = append(ret, strings.ToLower(s))
	}

	return ret
}

func main() {
	// arguments
	var debug = flag.Bool("debug", false, "Debug output")
	var methodStr = flag.String("methods", "CONNECT", "Allowed methods separated with comma.")
	var cert = flag.String("cert", "", "X509 Certificate")
	var privKey = flag.String("key", "", "Private key")
	var bufSize = flag.Int("buffer", 64000, "Buffer size")
	var credential = flag.String("auth", "", "<username>:<password> credential for proxy authorization")
	var port = flag.Int("port", 8080, "Listening port")
	flag.Parse()

	logLevel = log.InfoLevel
	if *debug {
		logLevel = log.DebugLevel
	}

	for _, s := range parseMethodsStr(*methodStr) {
		methods[s] = true
	}
	if len(methods) == 0 {
		println("Not enough methods value supplied...")
		os.Exit(1)
	}

	addr = fmt.Sprintf(":%d", *port)
	if *credential != "" {
		b64Cred = base64.StdEncoding.EncodeToString([]byte(*credential))
	}
	if *cert == "" {
		*cert = os.Getenv("TUNNEL_CERT")
	}
	if *privKey == "" {
		*privKey = os.Getenv("TUNNEL_KEY")
	}

	readerBufSize = *bufSize

	initLogger(logLevel)
	l, err := initListener(*cert, *privKey)
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
