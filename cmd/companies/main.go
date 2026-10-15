package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"example.com/companies/internal/app"
	"example.com/companies/internal/platform/config"
	"example.com/companies/internal/platform/logging"
	"example.com/companies/internal/platform/postgres"
	"example.com/companies/internal/platform/telemetry"
)

var (
	version   = "dev"
	errLogged = errors.New("already logged")
)

// @title						Companies service API
// @version					1.0
// @description				CRUD for companies. Reads are public; create, patch and delete need a bearer JWT (HS256).
// @description				Every mutation publishes an event to Kafka (companies.events.v1; refused events go to companies.events.v1.dlq).
// @BasePath					/
// @securityDefinitions.apikey	BearerAuth
// @in							header
// @name						Authorization
// @description				Enter "Bearer " followed by the JWT (make token).
func main() {
	err := run(os.Args[1:], os.Stdout)
	if ue, ok := errors.AsType[usageError](err); ok {
		fmt.Fprintf(os.Stderr, "error: %s\n\n%s", ue.msg, usage)
		os.Exit(exitUsage)
	}
	if err != nil {
		if !errors.Is(err, errLogged) {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		os.Exit(exitFailure)
	}
}

const (
	exitFailure = 1
	exitUsage   = 2
)

const usage = `Usage: companies [command]

Commands:
  serve                             run the HTTP API (default)
  migrate                           apply database migrations and exit
  healthcheck [ready|live|startup]  probe the local server, exit 0 when it answers 200
  version                           print the version
  help                              show this help

Configuration is read from COMPANIES_* environment variables (see .env.example).
`

var probePaths = map[string]string{"ready": "/readyz", "live": "/livez", "startup": "/startupz"}

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func run(args []string, stdout io.Writer) error {
	command, rest := "serve", []string(nil)
	if len(args) > 0 {
		command, rest = args[0], args[1:]
	}

	probe := "ready"
	switch command {
	case "help", "-h", "--help":
		_, err := io.WriteString(stdout, usage)
		return err
	case "version", "--version":
		_, err := fmt.Fprintln(stdout, version)
		return err
	case "serve", "migrate":
		if len(rest) > 0 {
			return usageError{command + " takes no arguments"}
		}
	case "healthcheck":
		if len(rest) > 1 {
			return usageError{"healthcheck takes at most one probe"}
		}
		if len(rest) == 1 {
			probe = rest[0]
		}
		if _, ok := probePaths[probe]; !ok {
			return usageError{fmt.Sprintf("unknown probe %q (expected ready, live or startup)", probe)}
		}
	default:
		return usageError{fmt.Sprintf("unknown command %q", command)}
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	context.AfterFunc(ctx, stop)

	switch command {
	case "migrate":
		return migrate(ctx, cfg)
	case "healthcheck":
		return healthcheck(ctx, cfg, probe)
	default:
		return serve(ctx, cfg)
	}
}

func serve(ctx context.Context, cfg config.Config) (err error) {
	tel, err := telemetry.Setup(ctx, cfg.Telemetry, version)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = errors.Join(err, tel.Shutdown(shutdownCtx))
	}()

	logger, err := logging.New(cfg.Log, telemetry.ServiceName, tel.LoggerProvider)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			logger.ErrorContext(ctx, "serve failed", slog.Any("error", err))
			err = fmt.Errorf("%w: %w", errLogged, err)
		}
	}()
	logger.InfoContext(ctx, "starting",
		slog.String("version", version),
		slog.Bool("telemetry", cfg.Telemetry.Enabled),
		slog.Int("gomaxprocs", runtime.GOMAXPROCS(0)),
		slog.Int("num_cpu", runtime.NumCPU()),
		slog.String("gomaxprocs_source", gomaxprocsSource()),
	)

	a, err := app.New(ctx, cfg, logger)
	if err != nil {
		return fmt.Errorf("start: %w", err)
	}
	defer a.Close()

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTP.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTP.Addr, err)
	}
	if err := a.Run(ctx, ln); err != nil {
		return fmt.Errorf("run: %w", err)
	}
	logger.InfoContext(ctx, "stopped")
	return nil
}

func migrate(ctx context.Context, cfg config.Config) error {
	pool, err := postgres.Connect(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer pool.Close()
	results, err := postgres.Migrate(ctx, pool)
	if err != nil {
		return err
	}
	for _, r := range results {
		fmt.Printf("applied %s (%s)\n", r.Source.Path, r.Duration)
	}
	fmt.Printf("%d migration(s) applied\n", len(results))
	return nil
}

func healthcheck(ctx context.Context, cfg config.Config, probe string) error {
	path := probePaths[probe]
	_, port, err := net.SplitHostPort(cfg.HTTP.Addr)
	if err != nil {
		return fmt.Errorf("parse %sHTTP_ADDR: %w", config.EnvPrefix, err)
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+path, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s probe failed: %s: %s", probe, resp.Status, body)
	}
	return nil
}

func gomaxprocsSource() string {
	if _, ok := os.LookupEnv("GOMAXPROCS"); ok {
		return "env"
	}
	return "runtime"
}
