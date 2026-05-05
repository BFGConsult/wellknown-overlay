package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/BFGConsult/wellknown-overlay/internal/overlay"
)

func main() {
	if err := run(os.Args, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) < 2 {
		usage(stderr)
		return errors.New("missing command")
	}

	switch args[1] {
	case "validate":
		return validate(args[2:], stdout)
	case "serve":
		return serve(args[2:], stderr)
	case "render":
		return render(args[2:], stdout)
	case "healthcheck":
		return healthcheck(args[2:])
	case "help", "-h", "--help":
		usage(stdout)
		return nil
	default:
		usage(stderr)
		return fmt.Errorf("unknown command %q", args[1])
	}
}

func validate(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", "overlay.json", "configuration file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := overlay.LoadConfig(*configPath)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "ok: %d route(s)\n", len(cfg.Routes))
	return nil
}

func serve(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", "overlay.json", "configuration file")
	root := fs.String("root", ".", "root directory for route files")
	listen := fs.String("listen", "127.0.0.1:8765", "listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}

	handler, err := newHandler(*configPath, *root)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              *listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		log.New(stderr, "", 0).Printf("serving overlay on http://%s", *listen)
		errc <- server.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-stop:
		log.New(stderr, "", 0).Printf("received %s, shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(ctx)
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func render(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", "overlay.json", "configuration file")
	root := fs.String("root", ".", "root directory for route files")
	routePath := fs.String("path", "", "route path to render")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *routePath == "" {
		return errors.New("missing -path")
	}

	responder, err := newResponder(*configPath, *root)
	if err != nil {
		return err
	}

	response, err := responder.Render(*routePath)
	if err != nil {
		return err
	}

	_, err = stdout.Write(response.Body)
	return err
}

func healthcheck(args []string) error {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	url := fs.String("url", "http://127.0.0.1:8765"+overlay.HealthPath, "health check URL")
	if err := fs.Parse(args); err != nil {
		return err
	}

	client := http.Client{
		Timeout: 2 * time.Second,
	}
	resp, err := client.Get(*url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed: status %d", resp.StatusCode)
	}

	return nil
}

func newHandler(configPath, root string) (http.Handler, error) {
	responder, err := newResponder(configPath, root)
	if err != nil {
		return nil, err
	}
	return overlay.NewHTTPHandler(responder), nil
}

func newResponder(configPath, root string) (*overlay.Responder, error) {
	cfg, err := overlay.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}

	if !filepath.IsAbs(root) {
		root, err = filepath.Abs(root)
		if err != nil {
			return nil, err
		}
	}

	return overlay.NewResponder(cfg, os.DirFS(root)), nil
}

func usage(w io.Writer) {
	fmt.Fprintln(w, `usage: wellknown-overlay <command> [options]

commands:
  validate  validate an overlay config
  serve     serve configured overlay routes over HTTP
  render    render one configured route to stdout
  healthcheck
            check an overlay HTTP endpoint`)
}
