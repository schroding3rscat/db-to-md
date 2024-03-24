package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func connect(ctx context.Context, host, port, db, user, password string) (pool *pgxpool.Pool, err error) {
	dsnMaster := fmt.Sprintf(
		"host=%s port=%s dbname=%s sslmode=disable user=%s password=%s standard_conforming_strings=on",
		host, port, db, user, password,
	)

	configMaster, err := pgxpool.ParseConfig(dsnMaster)
	if err != nil {
		return nil, err
	}

	configMaster.MaxConns = 4
	configMaster.MaxConnIdleTime = time.Minute
	configMaster.MaxConnLifetime = time.Minute

	return pgxpool.NewWithConfig(ctx, configMaster)
}
