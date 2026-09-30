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
		"Artificial per-variant processing delay. 0 by default -- set e.g. 1s only "+
			"when you deliberately want queue wait to be observable, such as the "+
			"five-image burst measurement in Week 4. Never leave this set for "+
			"timings meant to reflect real work.")

	flag.StringVar(&cfg.db.dsn, "db-dsn", "", "PostgreSQL DSN")
	flag.IntVar(&cfg.db.maxOpenConns, "db-max-open-conns", 25, "PostgreSQL max open connections")
	flag.IntVar(&cfg.db.maxIdleConns, "db-max-idle-conns", 25, "PostgreSQL max idle connections")
	flag.DurationVar(&cfg.db.maxIdleTime, "db-max-idle-time", 15*time.Minute, "PostgreSQL max connection idle time")

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
	w := worker.New(models, cfg.storage.root, logger, cfg.worker.delay)

	if cfg.worker.delay > 0 {
		logger.Info("worker artificial delay enabled", "delay", cfg.worker.delay)
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