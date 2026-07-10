package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrNoJWT indica que el archivo session.jwt no existe todavía (caso normal
// en el primer arranque tras la instalación).
var ErrNoJWT = errors.New("session jwt: file not found")

// jwtFilePath devuelve la ruta por defecto del archivo de sesión JWT:
// <directorio-del-config>/session.jwt. Si --jwt-file se pasó explícito,
// gana.
func jwtFilePath(cfgPath, jwtFileFlag string) string {
	if jwtFileFlag != "" {
		return jwtFileFlag
	}
	if cfgPath == "" {
		return ""
	}
	dir := filepath.Dir(cfgPath)
	return filepath.Join(dir, "session.jwt")
}

// loadJWT lee el session JWT del archivo. Devuelve (jwt, nil) si existe,
// ("", ErrNoJWT) si no existe (caso normal primer arranque), u otro error
// si el archivo existe pero no se puede leer.
func loadJWT(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrNoJWT
		}
		return "", fmt.Errorf("read session jwt %q: %w", path, err)
	}
	return string(b), nil
}

// saveJWT escribe el JWT al archivo con permisos 0600 (rw para owner only).
// Trunca el archivo antes de escribir. Crea el directorio si no existe.
// Es atómica: escribe a un temporal y hace rename, así un crash a mitad
// de escritura no deja un archivo corrupto.
func saveJWT(path, token string) error {
	if path == "" {
		return errors.New("session jwt: empty path")
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %q: %w", dir, err)
		}
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open tmp %q: %w", tmp, err)
	}
	if _, err := f.WriteString(token); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write tmp %q: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close tmp %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename tmp->%q: %w", path, err)
	}
	return nil
}

// clearJWT elimina el archivo de sesión JWT. No falla si no existe.
// Se usa cuando el server rechaza el JWT (e.g. secret rotado) y el agente
// debe re-enrolar.
func clearJWT(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove session jwt %q: %w", path, err)
	}
	return nil
}

// jwtFileMode devuelve los permisos del archivo. Usado en tests para
// validar el 0600.
func jwtFileMode(path string) (os.FileMode, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Mode().Perm(), nil
}