package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sofiworker/gk/ghttp/testdata/k6/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", "127.0.0.1:8080", "listen address")
	staticDir := fs.String("static-dir", "", "static fixture directory")
	maxBodyBytes := fs.Int64("max-body-bytes", 1<<20, "maximum request body bytes")
	secret := fs.String("secret", "known-k6-secret", "internal test secret")
	shutdownTimeout := fs.Duration("shutdown-timeout", 5*time.Second, "graceful shutdown timeout")
	shutdownFile := fs.String("shutdown-file", "", "file whose creation requests graceful shutdown")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *maxBodyBytes <= 0 {
		return fmt.Errorf("max-body-bytes must be positive")
	}
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", *addr, err)
	}
	serverCtx, cancelServer := context.WithCancel(ctx)
	defer cancelServer()
	if *shutdownFile != "" {
		_ = os.Remove(*shutdownFile)
		go func() {
			ticker := time.NewTicker(50 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-serverCtx.Done():
					return
				case <-ticker.C:
					if _, err := os.Stat(*shutdownFile); err == nil {
						cancelServer()
						return
					}
				}
			}
		}()
	}
	handler, cleanup := app.New(app.Config{StaticDir: *staticDir, MaxBodyBytes: *maxBodyBytes, Secret: *secret})
	defer cleanup()
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	fmt.Fprintf(stdout, "READY http://%s\n", listener.Addr().String())
	select {
	case err := <-serveDone:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	case <-serverCtx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), *shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-serveDone; err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
