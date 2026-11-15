// Package idempotency implements Idempotency-Key handling from server-handoff.md:
// the record holds subject, operation, key, canonical payload hash, status and result.
// Same key + same payload -> the stored result; same key + other payload -> 409.
// Only successful results are stored, so a failed attempt can be retried with the same key.
package idempotency

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/platform/httpx"
)

const MaxKeyLen = 200

// Key validates the Idempotency-Key header.
func Key(r *http.Request) (string, error) {
	k := r.Header.Get("Idempotency-Key")
	if k == "" || len(k) > MaxKeyLen {
		return "", httpx.Validation("Нужен заголовок Idempotency-Key (1–200 символов).", "Idempotency-Key")
	}
	return k, nil
}

// Hash returns the hash of the canonical payload (any JSON-serialisable value).
func Hash(payload any) (string, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), nil
}

// Replay is a previously stored successful result.
type Replay struct {
	Status int
	Body   []byte
}

// Begin locks the (subject, operation, key) slot inside tx and returns a stored result if any.
func Begin(ctx context.Context, tx *sql.Tx, subject, operation, key, hash string) (*Replay, error) {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		subject+"\x1f"+operation+"\x1f"+key); err != nil {
		return nil, err
	}
	var storedHash string
	var status int
	var body []byte
	err := tx.QueryRowContext(ctx, `SELECT payload_hash, status, response FROM idempotency_records
		WHERE subject=$1 AND operation=$2 AND key=$3`, subject, operation, key).Scan(&storedHash, &status, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if storedHash != hash {
		return nil, httpx.Conflict("IDEMPOTENCY_KEY_REUSED", "Этот ключ уже использован для другого запроса.", nil)
	}
	return &Replay{Status: status, Body: body}, nil
}

// Save stores the successful result in the same transaction as the change itself.
func Save(ctx context.Context, tx *sql.Tx, subject, operation, key, hash string, status int, result any) ([]byte, error) {
	body, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO idempotency_records(subject, operation, key, payload_hash, status, response)
		VALUES ($1,$2,$3,$4,$5,$6)`, subject, operation, key, hash, status, body)
	return body, err
}

// Purge deletes stored results older than keep. Retention was an open question
// (server-handoff.md): a week covers any client retry, and the table no longer grows forever.
func Purge(ctx context.Context, d *sql.DB, keep time.Duration) (int64, error) {
	res, err := d.ExecContext(ctx, `DELETE FROM idempotency_records WHERE created_at < now() - $1::interval`,
		fmt.Sprintf("%d seconds", int64(keep.Seconds())))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PurgeLoop runs Purge every interval until ctx ends.
func PurgeLoop(ctx context.Context, d *sql.DB, keep, interval time.Duration, log *slog.Logger) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if n, err := Purge(ctx, d, keep); err != nil && ctx.Err() == nil {
			log.Error("idempotency purge", "err", err)
		} else if n > 0 {
			log.Info("idempotency purge", "deleted", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
