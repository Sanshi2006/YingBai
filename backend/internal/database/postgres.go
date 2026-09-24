package database

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database configuration: %w", err)
	}
	config.MaxConns = 10
	config.MinConns = 1
	config.MaxConnLifetime = 30 * time.Minute
	bootstrapConfig := config.Copy()
	bootstrapConfig.MinConns = 0
	pool, err := pgxpool.NewWithConfig(ctx, bootstrapConfig)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	if err := migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	pool.Close()

	config.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		return pgxvec.RegisterTypes(ctx, connection)
	}
	pool, err = pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create vector-enabled database pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect vector-enabled PostgreSQL pool: %w", err)
	}
	return pool, nil
}

func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read database migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		sql, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}
	}
	return nil
}
