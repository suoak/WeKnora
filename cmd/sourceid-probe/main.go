// sourceid-probe is an isolated, single-transaction protocol observer.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/logger"
)

func main() { os.Exit(run()) }
func run() int {
	logger.SetOutput(io.Discard)
	if !disableCoreDumps() {
		return 2
	}
	metadata := flag.String("metadata", "", "operator-confirmed public metadata JSON")
	listen := flag.String("listen", "127.0.0.1:18183", "loopback listener")
	flag.Parse()
	host, _, err := net.SplitHostPort(*listen)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() || *metadata == "" {
		return 2
	}
	f, err := os.Open(*metadata)
	if err != nil {
		return 2
	}
	var m service.OIDCProbeMetadata
	decoder := json.NewDecoder(io.LimitReader(f, 64<<10))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&m)
	_ = f.Close()
	if err != nil {
		return 2
	}
	if service.ValidateOIDCProbeMetadata(m) != nil {
		return 2
	}
	// Require a pipe, preventing accidental echoed terminal input. The caller can
	// use a shell's hidden `read -s` and builtin printf without a file or argv secret.
	stat, err := os.Stdin.Stat()
	if err != nil || stat.Mode()&os.ModeNamedPipe == 0 {
		return 2
	}
	secret, err := bufio.NewReader(io.LimitReader(os.Stdin, 4098)).ReadBytes('\n')
	if err != nil || len(secret) > 4097 {
		return 2
	}
	value := strings.TrimSuffix(strings.TrimSuffix(string(secret), "\n"), "\r")
	p, err := service.NewOIDCProtocolProbe(m, value)
	for i := range secret {
		secret[i] = 0
	}
	if err != nil {
		return 2
	}
	defer p.Close()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	deadline, stop := context.WithTimeout(ctx, 10*time.Minute)
	defer stop()
	server := &http.Server{
		Handler:           p,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      120 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	completed := make(chan struct{})
	go func() {
		select {
		case <-p.Done():
		case <-deadline.Done():
		}
		p.Close()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
		close(completed)
	}()
	// Public endpoint only: no transaction state, code, credential or authorize URL.
	_ = json.NewEncoder(os.Stdout).
		Encode(map[string]interface{}{
			"endpoint": map[string]string{
				"probe_start": "http://" +
					*listen +
					"/probe/start",
			},
		})
	err = server.Serve(listener)
	p.Close()
	<-completed
	_ = json.NewEncoder(os.Stdout).Encode(p.Report())
	if err != nil && err != http.ErrServerClosed {
		return 1
	}
	if !p.Report().Validation["completed"] {
		return 1
	}
	return 0
}
