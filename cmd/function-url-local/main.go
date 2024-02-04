package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	httplogger "github.com/shogo82148/go-http-logger"
	"github.com/shogo82148/lambtrip"
)

var host, port string
var logHandler slog.Handler
var logger *slog.Logger

func init() {
	flag.StringVar(&host, "host", "", "host to forward requests to")
	flag.StringVar(&port, "port", "8080", "port to listen on")

	logHandler = slog.NewJSONHandler(os.Stderr, nil)
	logger = slog.New(logHandler)
	slog.SetDefault(logger)
}

func main() {
	ctx := context.Background()

	// parse flags
	flag.Parse()
	if flag.NArg() < 1 {
		slog.ErrorContext(ctx, "function name is required")
		os.Exit(1)
	}
	functionName := flag.Arg(0)

	// initialize AWS SDK
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to load configuration", slog.String("error", err.Error()))
		os.Exit(1)
	}
	svc := lambda.NewFromConfig(cfg)

	// create a reverse proxy
	t := lambtrip.NewTransport(svc)
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Host = functionName
		},
		Transport: t,
		ErrorLog:  slog.NewLogLogger(logHandler, slog.LevelWarn),
	}
	myLogger := httplogger.NewSlogLogger(slog.LevelInfo, "request", logger)
	handler := httplogger.LoggingHandler(myLogger, proxy)

	// start the server
	addr := net.JoinHostPort(host, port)
	if err := http.ListenAndServe(addr, handler); err != nil {
		slog.ErrorContext(ctx, "failed to start server", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
