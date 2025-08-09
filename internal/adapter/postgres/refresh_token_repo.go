package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

type RefreshTokenRepo struct{ pool *Pool }

func NewRefreshTokenRepo(pool *Pool) *RefreshTokenRepo { return &RefreshTokenRepo{pool: pool} }

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (r *RefreshTokenRepo) Store(ctx context.Context, userID, token string, expiresAt time.Time) error {
	_, err := r.pool.DB.Exec(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1,$2,$3)`,
		userID, hashToken(token), expiresAt,
	)
	return err
}

type RefreshTokenRow struct {
	UserID    string
	ExpiresAt time.Time
	RevokedAt *time.Time
}

func (r *RefreshTokenRepo) GetByToken(ctx context.Context, token string) (RefreshTokenRow, error) {
	var row RefreshTokenRow
	err := r.pool.DB.QueryRow(ctx,
		`SELECT user_id, expires_at, revoked_at FROM refresh_tokens WHERE token_hash=$1 ORDER BY created_at DESC LIMIT 1`,
		hashToken(token),
	).Scan(&row.UserID, &row.ExpiresAt, &row.RevokedAt)
	return row, err
}

func (r *RefreshTokenRepo) Rotate(ctx context.Context, oldToken, newToken string, newExpiresAt time.Time) error {
	// mark old as revoked and insert new
	_, err := r.pool.DB.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at=now() WHERE token_hash=$1`, hashToken(oldToken),
	)
	if err != nil {
		return err
	}
	_, err = r.pool.DB.Exec(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) SELECT user_id, $1, $2 FROM refresh_tokens WHERE token_hash=$3 LIMIT 1`,
		hashToken(newToken), newExpiresAt, hashToken(oldToken),
	)
	return err
}
