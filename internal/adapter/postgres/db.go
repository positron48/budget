package postgres

import (
    "context"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
)

type Pool struct {
    DB *pgxpool.Pool
}

func NewPool(ctx context.Context, databaseURL string) (*Pool, error) {
    cfg, err := pgxpool.ParseConfig(databaseURL)
    if err != nil {
        return nil, err
    }
    cfg.MaxConns = 10
    cfg.MinConns = 1
    cfg.MaxConnIdleTime = 5 * time.Minute
    cfg.MaxConnLifetime = 60 * time.Minute
    db, err := pgxpool.NewWithConfig(ctx, cfg)
    if err != nil {
        return nil, err
    }
    return &Pool{DB: db}, nil
}

func (p *Pool) Ping(ctx context.Context) error {
    return p.DB.Ping(ctx)
}

func (p *Pool) Close() {
    p.DB.Close()
}


