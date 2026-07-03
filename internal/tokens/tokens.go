// Package tokens administra los enrollment tokens: creación, listado,
// revocación y canje (cuando un agente hace hello con un token válido).
package tokens

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Token representa un enrollment token registrado.
type Token struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	CreatedBy  string     `json:"created_by"`
	MaxUses    int        `json:"max_uses"`
	Uses       int        `json:"uses"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	HasUsesLeft bool      `json:"has_uses_left"`
}

// Create crea un token nuevo y devuelve el token en plaintext (solo se
// muestra una vez al admin).
func Create(ctx context.Context, pool *pgxpool.Pool, label, createdBy string, maxUses int, ttl time.Duration) (tokenPlain string, t *Token, err error) {
	if maxUses < 1 {
		maxUses = 1
	}
	plain, err := generatePlain()
	if err != nil {
		return "", nil, err
	}
	hash := hashToken(plain)
	var exp *time.Time
	if ttl > 0 {
		e := time.Now().Add(ttl)
		exp = &e
	}

	row := pool.QueryRow(ctx, `
		INSERT INTO enrollment_tokens (token_hash, label, created_by, max_uses, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, label, created_by, max_uses, uses, expires_at, revoked_at, created_at
	`, hash, label, createdBy, maxUses, exp)
	t = &Token{}
	if err := row.Scan(&t.ID, &t.Label, &t.CreatedBy, &t.MaxUses, &t.Uses, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt); err != nil {
		return "", nil, fmt.Errorf("insert token: %w", err)
	}
	t.HasUsesLeft = t.HasUses()
	return plain, t, nil
}

// List devuelve todos los tokens ordenados por creación descendente.
func List(ctx context.Context, pool *pgxpool.Pool) ([]*Token, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, label, created_by, max_uses, uses, expires_at, revoked_at, created_at
		FROM enrollment_tokens ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Token
	for rows.Next() {
		t := &Token{}
		if err := rows.Scan(&t.ID, &t.Label, &t.CreatedBy, &t.MaxUses, &t.Uses, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.HasUsesLeft = t.HasUses()
		out = append(out, t)
	}
	return out, rows.Err()
}

// Get devuelve un token por ID.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (*Token, error) {
	row := pool.QueryRow(ctx, `
		SELECT id, label, created_by, max_uses, uses, expires_at, revoked_at, created_at
		FROM enrollment_tokens WHERE id = $1
	`, id)
	t := &Token{}
	if err := row.Scan(&t.ID, &t.Label, &t.CreatedBy, &t.MaxUses, &t.Uses, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	t.HasUsesLeft = t.HasUses()
	return t, nil
}

// Revoke marca el token como revocado.
func Revoke(ctx context.Context, pool *pgxpool.Pool, id string) error {
	tag, err := pool.Exec(ctx, `
		UPDATE enrollment_tokens SET revoked_at = now()
		WHERE id = $1 AND revoked_at IS NULL
	`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ConsumeResult es lo que devuelve Canje: el token + si fue exitoso.
type ConsumeResult struct {
	TokenID string
}

// Redeem canjea un token plaintext por su registro (verificando que
// esté activo). Incrementa `uses` atómicamente.
func Redeem(ctx context.Context, pool *pgxpool.Pool, plain string) (*ConsumeResult, error) {
	if strings.TrimSpace(plain) == "" {
		return nil, ErrInvalid
	}
	hash := hashToken(plain)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		id       string
		maxUses  int
		uses     int
		expAt    *time.Time
		revoked  *time.Time
	)
	row := tx.QueryRow(ctx, `
		SELECT id, max_uses, uses, expires_at, revoked_at
		FROM enrollment_tokens WHERE token_hash = $1
		FOR UPDATE
	`, hash)
	if err := row.Scan(&id, &maxUses, &uses, &expAt, &revoked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if revoked != nil {
		return nil, ErrRevoked
	}
	if expAt != nil && time.Now().After(*expAt) {
		return nil, ErrExpired
	}
	if uses >= maxUses {
		return nil, ErrExhausted
	}
	if _, err := tx.Exec(ctx, `UPDATE enrollment_tokens SET uses = uses + 1 WHERE id = $1`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &ConsumeResult{TokenID: id}, nil
}

// HasUses devuelve si el token todavía puede ser usado.
func (t *Token) HasUses() bool {
	if t.RevokedAt != nil {
		return false
	}
	if t.ExpiresAt != nil && time.Now().After(*t.ExpiresAt) {
		return false
	}
	return t.Uses < t.MaxUses
}

// Errores públicos.
var (
	ErrNotFound  = errors.New("token not found")
	ErrInvalid   = errors.New("token invalid")
	ErrRevoked   = errors.New("token revoked")
	ErrExpired   = errors.New("token expired")
	ErrExhausted = errors.New("token exhausted")
)

// -----------------------------------------------------------------------------
// Internals
// -----------------------------------------------------------------------------

// generatePlain devuelve un token URL-safe de 32 bytes (256 bits) de entropía.
func generatePlain() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken devuelve SHA-256 del token en hex.
func hashToken(plain string) string {
	h := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(h[:])
}

// HashPlain expone el hash para que el download endpoint valide el token
// entrante sin re-implementar la lógica.
func HashPlain(plain string) string { return hashToken(plain) }