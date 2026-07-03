// Package auth maneja la autenticación de administradores del panel:
// hashing con Argon2id, JWT de sesión, sesiones persistidas y CSRF.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/argon2"
)

// ErrInvalidCredentials se devuelve cuando el email no existe o el hash no coincide.
var ErrInvalidCredentials = errors.New("invalid credentials")

// Argon2id parameters (recomendación OWASP 2024).
const (
	a2Memory    uint32 = 64 * 1024
	a2Iterations uint32 = 3
	a2Parallelism uint8 = 2
	a2SaltLen   uint32 = 16
	a2KeyLen    uint32 = 32
)

// HashPassword hashea una contraseña con Argon2id y devuelve el string
// codificado en formato estándar `$argon2id$v=19$m=...,t=...,p=...$salt$hash`.
func HashPassword(password string) (string, error) {
	salt := make([]byte, a2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, a2Iterations, a2Memory, a2Parallelism, a2KeyLen)
	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Key := base64.RawStdEncoding.EncodeToString(key)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, a2Memory, a2Iterations, a2Parallelism, b64Salt, b64Key), nil
}

// VerifyPassword compara una contraseña contra un hash Argon2id.
// Devuelve nil si coincide.
func VerifyPassword(password, encoded string) error {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return errors.New("invalid argon2 hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return fmt.Errorf("parse version: %w", err)
	}
	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return fmt.Errorf("parse params: %w", err)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return fmt.Errorf("decode salt: %w", err)
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return fmt.Errorf("decode hash: %w", err)
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrInvalidCredentials
	}
	return nil
}

// -----------------------------------------------------------------------------
// JWT de admin
// -----------------------------------------------------------------------------

// Claims claims del JWT de admin.
type Claims struct {
	UserID string `json:"uid"`
	Email  string `json:"eml"`
	Role   string `json:"rol"`
	CSRF   string `json:"csrf"`
	jwt.RegisteredClaims
}

// IssueJWT genera un JWT firmado para un admin.
func IssueJWT(secret string, userID, email, role, csrf string, ttl time.Duration) (string, time.Time, error) {
	exp := time.Now().Add(ttl)
	claims := Claims{
		UserID: userID,
		Email:  email,
		Role:   role,
		CSRF:   csrf,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "sai",
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, exp, nil
}

// ParseJWT valida y devuelve los claims del token.
func ParseJWT(secret, raw string) (*Claims, error) {
	c := &Claims{}
	tok, err := jwt.ParseWithClaims(raw, c, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	if !tok.Valid {
		return nil, errors.New("invalid token")
	}
	return c, nil
}

// -----------------------------------------------------------------------------
// Sesiones (persistidas en DB)
// -----------------------------------------------------------------------------

// Session representa una sesión activa del panel.
type Session struct {
	ID        string
	UserID    string
	CSRFToken string
	UserAgent string
	IP        string
	ExpiresAt time.Time
}

// CreateSession crea una sesión nueva y devuelve el session_id + csrf + expiry.
func CreateSession(ctx context.Context, pool *pgxpool.Pool, userID, userAgent, ip string, ttl time.Duration) (sessionID, csrf string, expires time.Time, err error) {
	sessionID = newToken(32)
	csrf = newToken(32)
	expires = time.Now().Add(ttl)
	_, err = pool.Exec(ctx, `
		INSERT INTO sessions (id, user_id, csrf_token, user_agent, ip, expires_at)
		VALUES ($1, $2, $3, $4, NULLIF($5,'')::inet, $6)
	`, sessionID, userID, csrf, userAgent, ip, expires)
	return
}

// LookupSession devuelve la sesión si existe y no expiró.
func LookupSession(ctx context.Context, pool *pgxpool.Pool, sessionID string) (*Session, error) {
	row := pool.QueryRow(ctx, `
		SELECT id, user_id, csrf_token, COALESCE(user_agent,''), COALESCE(host(ip),''),
		       expires_at
		FROM sessions WHERE id = $1 AND expires_at > now()
	`, sessionID)
	s := &Session{}
	if err := row.Scan(&s.ID, &s.UserID, &s.CSRFToken, &s.UserAgent, &s.IP, &s.ExpiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	return s, nil
}

// DeleteSession elimina una sesión.
func DeleteSession(ctx context.Context, pool *pgxpool.Pool, sessionID string) error {
	_, err := pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, sessionID)
	return err
}

// PurgeExpiredSessions borra sesiones vencidas (para cron interno).
func PurgeExpiredSessions(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	tag, err := pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= now()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

func newToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}