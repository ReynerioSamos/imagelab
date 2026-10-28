package main

import (
	"context"
	"database/sql"
	"flag"
	"log/slog"
	"os"
	"time"

	"imagelab/internal/data"
	"imagelab/internal/worker"

	_ "github.com/lib/pq"
)

type config struct {
	port    int
	env     string
	storage struct {
		root string
	}
	upload struct {
		maxBytes int64
	}
	// new worker delay
	worker struct {
		delay time.Duration
	}
	db struct {
		dsn          string
		maxOpenConns int
		maxIdleConns int
		maxIdleTime  time.Duration
	}
	// flags needed for experiment
	experiment struct {
		quietDelay         time.Duration
		simulationInterval time.Duration
	}
}

type application struct {
	config config
	logger *slog.Logger
	models data.Models
	worker *worker.Worker
}

func main() {
	var cfg config

	flag.IntVar(&cfg.port, "port", 4000, "API server port")
	flag.StringVar(&cfg.env, "env", "development", "Environment (development|staging|production)")
	flag.StringVar(&cfg.storage.root, "storage-root", "./storage", "Filesystem root for original and variant images")
	flag.Int64Var(&cfg.upload.maxBytes, "max-upload-bytes", 10<<20, "Maximum accepted upload size in bytes (VAL-01: 10 MB)")
	// worker delay flag in server start logs
	flag.DurationVar(&cfg.worker.delay, "worker-delay", 0,
		"Artificial per-variant processing delay. 0 by default")

	flag.StringVar(&cfg.db.dsn, "db-dsn", "", "PostgreSQL DSN")
	flag.IntVar(&cfg.db.maxOpenConns, "db-max-open-conns", 25, "PostgreSQL max open connections")
	flag.IntVar(&cfg.db.maxIdleConns, "db-max-idle-conns", 25, "PostgreSQL max idle connections")
	flag.DurationVar(&cfg.db.maxIdleTime, "db-max-idle-time", 15*time.Minute, "PostgreSQL max connection idle time")
	flag.DurationVar(&cfg.experiment.quietDelay, "exp-quiet-delay", 0, "Condition A: 30s quiet delay with no intermediate updates")
	flag.DurationVar(&cfg.experiment.simulationInterval, "exp-simulation-interval", 0, "Condition C1/C2/C3: Simulated update phase interval (e.g. 2s or 200ms)")

	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	db, err := openDB(cfg)
	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
	defer db.Close()

	logger.Info("database connection pool established")

	if err := ensureStorageDirs(cfg.storage.root); err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}

	models := data.NewModels(db)
	// server logs show worker delay
	// Test 2 - Updated to pass a callback to the worker that calls globalHub.NotifyAll after a successful transaction commit
	// used so it can wake up long-polling observers after a job state change
	w := worker.New(
		models,
		cfg.storage.root,
		logger,
		cfg.worker.delay,
		cfg.experiment.quietDelay,
		cfg.experiment.simulationInterval,
		func(jobID string) {
			globalHub.NotifyAll(jobID)
		},
	)

	if cfg.worker.delay > 0 {
		logger.Info("worker artificial delay enabled", "delay", cfg.worker.delay)
	}
	if cfg.experiment.quietDelay > 0 {
		logger.Info("experiment quiet delay enabled", "quiet_delay", cfg.experiment.quietDelay)
	}
	if cfg.experiment.simulationInterval > 0 {
		logger.Info("experiment simulation interval enabled", "interval", cfg.experiment.simulationInterval)
	}

	app := &application{
		config: cfg,
		logger: logger,
		models: models,
		worker: w,
	}

	if err := app.serve(); err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}

func openDB(cfg config) (*sql.DB, error) {
	db, err := sql.Open("postgres", cfg.db.dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(cfg.db.maxOpenConns)
	db.SetMaxIdleConns(cfg.db.maxIdleConns)
	db.SetConnMaxIdleTime(cfg.db.maxIdleTime)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}