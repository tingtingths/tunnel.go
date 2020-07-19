# tunnel.go

[HTTP **CONNECT** tunnel](https://developer.mozilla.org/en-US/docs/Web/HTTP/Methods/CONNECT).  
  
Features
- HTTP Basic authorization
- Proxy client encryption

Usage
```sh
$ go build -o tunnel . && ./tunnel --help # all parameters are optional
Usage of ./tunnel:
  -auth string
        <username>:<password> credential for proxy authorization
  -buffer int
        Buffer size (default 64000)
  -cert string
        X509 Certificate
  -debug
        Debug output
  -key string
        Private key
  -methods string
        Allowed methods separated with comma. Default "CONNECT" only. (default "CONNECT")
  -port int
        Listening port (default 8080)
```

Or with Docker
```sh
docker run -td --name tunnel \
--restart=always \
-p 8080:8080 \
-v <path/to/cert>:/config/cert \
-v <path/to/key>:/config/key \
tingtingths/http_tunnel:latest \
-auth <username>:<password>
```

then in your `~/.ssh/config`, with [proxytunnel](https://github.com/proxytunnel/proxytunnel)
```
Host *
    ProxyCommand /usr/local/bin/proxytunnel -P <username>:<password> -z -E -p <proxy_host>:<proxy_port> -d %h:%p
```
