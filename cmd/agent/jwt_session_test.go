package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadJWT_NotFoundReturnsErrNoJWT(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jwt")

	_, err := loadJWT(path)
	if !errors.Is(err, ErrNoJWT) {
		t.Fatalf("expected ErrNoJWT, got %v", err)
	}
}

func TestLoadAndSaveRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jwt")

	const want = "eyJhbGciOiJIUzI1NiJ9.payload.signature"
	if err := saveJWT(path, want); err != nil {
		t.Fatalf("saveJWT: %v", err)
	}
	got, err := loadJWT(path)
	if err != nil {
		t.Fatalf("loadJWT: %v", err)
	}
	if got != want {
		t.Fatalf("loadJWT = %q, want %q", got, want)
	}
}

func TestSaveJWT_OverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jwt")

	if err := saveJWT(path, "old-token-aaaaaaaaaaaaaa"); err != nil {
		t.Fatalf("save 1: %v", err)
	}
	if err := saveJWT(path, "new-token-bbbbbbbbbbbbbb"); err != nil {
		t.Fatalf("save 2: %v", err)
	}
	got, err := loadJWT(path)
	if err != nil {
		t.Fatalf("loadJWT: %v", err)
	}
	if !strings.HasPrefix(got, "new-token-") {
		t.Fatalf("expected new-token-..., got %q", got)
	}
}

func TestSaveJWT_CreatesParentDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "nested", "session.jwt")

	if err := saveJWT(path, "token"); err != nil {
		t.Fatalf("saveJWT should create nested dirs: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should exist: %v", err)
	}
}

func TestClearJWT_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jwt")

	// clear sobre archivo inexistente -> nil
	if err := clearJWT(path); err != nil {
		t.Fatalf("clearJWT on missing file: %v", err)
	}
	// save y luego clear
	if err := saveJWT(path, "x"); err != nil {
		t.Fatalf("saveJWT: %v", err)
	}
	if err := clearJWT(path); err != nil {
		t.Fatalf("clearJWT on existing file: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected file gone, got err=%v", err)
	}
}

func TestClearJWT_EmptyPath(t *testing.T) {
	if err := clearJWT(""); err != nil {
		t.Fatalf("clearJWT empty path should be no-op: %v", err)
	}
}

// TestSaveJWT_FilePermissions0600 verifica que el archivo se crea con
// permisos 0600 (rw para owner only). En Windows el concepto de permisos
// POSIX no aplica igual; skip.
func TestSaveJWT_FilePermissions0600(t *testing.T) {
	if os.Getenv("OS") == "Windows_NT" || filepath.Separator == '\\' {
		t.Skip("POSIX permissions not meaningful on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jwt")
	if err := saveJWT(path, "secret"); err != nil {
		t.Fatalf("saveJWT: %v", err)
	}
	mode, err := jwtFileMode(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode != 0o600 {
		t.Fatalf("file mode = %o, want 0600", mode)
	}
}

func TestJWTFilePath_DefaultFromConfig(t *testing.T) {
	got := jwtFilePath("/etc/sai/agent/config.json", "")
	want := filepath.FromSlash("/etc/sai/agent/session.jwt")
	if got != want {
		t.Fatalf("default path = %q, want %q", got, want)
	}
}

func TestJWTFilePath_FlagOverrides(t *testing.T) {
	got := jwtFilePath("/etc/sai/agent/config.json", "/var/lib/sai/jwt")
	if got != "/var/lib/sai/jwt" {
		t.Fatalf("flag path = %q, want %q", got, "/var/lib/sai/jwt")
	}
}

func TestJWTFilePath_EmptyConfigAndFlag(t *testing.T) {
	if got := jwtFilePath("", ""); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}