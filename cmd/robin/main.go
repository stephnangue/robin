// Command robin is a workload-identity injector: a thin reverse proxy that
// sources the workload's native identity and injects it as a bearer token on
// outbound requests to a credential broker.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/snangue/robin/internal/config"
	"github.com/snangue/robin/internal/identity"
	"github.com/snangue/robin/internal/obs"
	"github.com/snangue/robin/internal/proxy"
	"github.com/snangue/robin/internal/server"
	"github.com/snangue/robin/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "robin:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	for _, a := range args {
		switch a {
		case "-v", "--version", "version":
			fmt.Println("robin", version.String())
			return nil
		case "-h", "--help", "help":
			fmt.Println(usage)
			return nil
		}
	}

	cfg, err := config.Load(args, os.Getenv)
	if err != nil {
		return err
	}
	log := obs.NewLogger(cfg.LogLevel)

	// SIGTERM/SIGINT cancel the context; the server then drains in-flight egress.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	provider, err := identity.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = provider.Close() }()

	handler, err := proxy.New(cfg, provider, log)
	if err != nil {
		return err
	}

	srv, err := server.New(cfg, handler, server.AdminMux(provider), log)
	if err != nil {
		return err
	}
	return srv.Run(ctx)
}

const usage = `robin — workload-identity injector

Robin injects the workload's native identity as a bearer token on every
outbound request to a credential broker. Configure via ROBIN_* environment
variables (see docs/design.md); precedence is flags > env > file.

  --version              print version and exit
  --help                 show this help
  --config PATH          flat KEY=value config file
  --upstream-url URL     broker base URL (ROBIN_UPSTREAM_URL, required)
  --token-source SRC     file | jwtsvid (ROBIN_TOKEN_SOURCE)
  --listen-addr ADDR     proxy listen address (ROBIN_LISTEN_ADDR)
  --admin-addr ADDR      admin/health listen address (ROBIN_ADMIN_ADDR)`
