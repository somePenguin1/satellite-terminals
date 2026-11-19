// Command ingest receives terminal telemetry over gRPC and WebSockets and writes it to Postgres.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"satfleet/ingest/internal/grpcapi"
	"satfleet/ingest/internal/httpapi"
	"satfleet/ingest/internal/ingest"
	"satfleet/ingest/internal/live"
	"satfleet/ingest/internal/pipeline"
	"satfleet/ingest/internal/registry"
	"satfleet/ingest/internal/store"
	"satfleet/ingest/internal/telemetry"
)

type config struct {
	DatabaseURL     string
	GRPCAddr        string
	HTTPAddr        string
	Token           string
	AllowedOrigins  []string
	DBMaxConns      int32
	Pipeline        pipeline.Config
	RegistryRefresh time.Duration
	RollupFlush     time.Duration
	LogLevel        slog.Level
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	n, err := strconv.Atoi(env(key, strconv.Itoa(def)))
	if err != nil {
		return def
	}
	return n
}

func envDuration(key string, def time.Duration) time.Duration {
	d, err := time.ParseDuration(env(key, def.String()))
	if err != nil {
		return def
	}
	return d
}

func loadConfig() config {
	p := pipeline.DefaultConfig()
	p.Shards = envInt("PIPELINE_SHARDS", p.Shards)
	p.BatchSize = envInt("BATCH_SIZE", p.BatchSize)
	p.QueueSize = envInt("QUEUE_SIZE", p.QueueSize)
	p.FlushInterval = envDuration("FLUSH_INTERVAL", p.FlushInterval)

	var level slog.Level
	_ = level.UnmarshalText([]byte(env("LOG_LEVEL", "info")))
	var origins []string
	for _, o := range strings.Split(env("ALLOWED_ORIGINS", ""), ",") {
		if o = strings.TrimSpace(o); o != "" {
			origins = append(origins, o)
		}
	}
	return config{
		DatabaseURL:     env("DATABASE_URL", "postgres://fleet:fleet@localhost:5432/fleet"),
		GRPCAddr:        env("GRPC_ADDR", ":9090"),
		HTTPAddr:        env("HTTP_ADDR", ":8081"),
		Token:           env("INGEST_TOKEN", ""),
		AllowedOrigins:  origins,
		DBMaxConns:      int32(envInt("DB_MAX_CONNS", p.Shards*2+4)),
		Pipeline:        p,
		RegistryRefresh: envDuration("REGISTRY_REFRESH", 15*time.Second),
		RollupFlush:     envDuration("ROLLUP_FLUSH", 5*time.Second),
		LogLevel:        level,
	}
}

// healthcheck lets distroless containers probe themselves without curl.
func healthcheck(addr string) int {
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + addr + "/readyz")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

// retry runs fn until it succeeds or ctx ends; the database and its schema (owned by the
// fleet API's migrations) may come up after this service does.
func retry(ctx context.Context, log *slog.Logger, what string, fn func(context.Context) error) error {
	delay := 500 * time.Millisecond
	for {
		err := fn(ctx)
		if err == nil {
			return nil
		}
		log.Warn("waiting for dependency", "what", what, "err", err, "retry_in", delay)
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: %w", what, err)
		case <-time.After(delay):
		}
		delay = min(delay*2, 10*time.Second)
	}
}

func main() {
	check := flag.Bool("healthcheck", false, "probe /readyz on HTTP_ADDR and exit")
	flag.Parse()
	cfg := loadConfig()
	if *check {
		os.Exit(healthcheck(cfg.HTTPAddr))
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	if err := run(cfg, log); err != nil {
		log.Error("ingest stopped", "err", err)
		os.Exit(1)
	}
}

func run(cfg config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	startup, cancelStartup := context.WithTimeout(ctx, 3*time.Minute)
	defer cancelStartup()
	var db *store.Store
	if err := retry(startup, log, "postgres", func(c context.Context) (err error) {
		db, err = store.Open(c, cfg.DatabaseURL, cfg.DBMaxConns)
		return err
	}); err != nil {
		return err
	}
	defer db.Close()

	reg := registry.New(db)
	if err := retry(startup, log, "terminal registry (schema migrated?)", reg.Refresh); err != nil {
		return err
	}
	log.Info("terminal registry loaded", "terminals", reg.Size())

	m := ingest.NewMetrics()
	svc := ingest.New(ingest.Deps{
		Log: log, Validator: telemetry.DefaultValidator(), Registry: reg, Sink: db,
		Pipeline: cfg.Pipeline, Hub: live.NewHub(), Metrics: m,
	})
	svc.Start()

	// Background loops: registry refresh, live ticks, rollup flushing.
	bg, cancelBG := context.WithCancel(context.Background())
	var bgWG sync.WaitGroup
	bgWG.Add(2)
	go func() { defer bgWG.Done(); reg.Run(bg, cfg.RegistryRefresh, log) }()
	go func() { defer bgWG.Done(); svc.RunTicker(bg) }()
	rollupCtx, stopRollups := context.WithCancel(context.Background())
	rollupDone := make(chan struct{})
	go func() { defer close(rollupDone); svc.Rollups().Run(rollupCtx, cfg.RollupFlush, db, log) }()

	grpcServer, health := grpcapi.New(svc, cfg.Token)
	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return err
	}
	// WebSocket connections are hijacked, so http.Server.Shutdown won't end them; tie their
	// request contexts to connCtx and cancel it during shutdown instead.
	connCtx, cancelConns := context.WithCancel(context.Background())
	defer cancelConns()
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		BaseContext:       func(net.Listener) context.Context { return connCtx },
		Handler:           httpapi.NewHandler(svc, httpapi.Options{Token: cfg.Token, AllowedOrigins: cfg.AllowedOrigins, Ping: db.Ping, Log: log}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errs := make(chan error, 2)
	go func() { errs <- grpcServer.Serve(lis) }()
	go func() {
		if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()
	health.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	log.Info("ingest ready", "grpc", cfg.GRPCAddr, "http", cfg.HTTPAddr, "shards", cfg.Pipeline.Shards,
		"batch_size", cfg.Pipeline.BatchSize, "auth", cfg.Token != "")

	select {
	case <-ctx.Done():
		log.Info("shutting down")
	case err := <-errs:
		log.Error("server failed", "err", err)
	}

	// Drain order: stop taking traffic, flush queued reports, then flush rollups that include them.
	health.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	shutdown, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	stopped := make(chan struct{})
	go func() { grpcServer.GracefulStop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		grpcServer.Stop() // long-lived streams: cut them after a grace period
	}
	_ = httpServer.Shutdown(shutdown)
	cancelConns()
	if err := svc.Close(shutdown); err != nil {
		log.Error("pipeline drain incomplete", "err", err)
	}
	cancelBG()
	bgWG.Wait()
	stopRollups()
	<-rollupDone
	log.Info("stopped cleanly", "persisted", m.Persisted.Value())
	return nil
}
