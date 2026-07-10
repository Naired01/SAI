//go:build !windows

package main

import (
	"context"
	"log/slog"
)

// isWindowsService es un stub en plataformas no-Windows.
func isWindowsService() bool { return false }

// runAsService es un stub en plataformas no-Windows.
func runAsService(name string, ctx context.Context, cancel context.CancelFunc, logger *slog.Logger, cfg *Config, hostname string) (bool, error) {
	return false, nil
}