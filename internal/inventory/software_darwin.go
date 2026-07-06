//go:build darwin

package inventory

import (
	"bufio"
	"bytes"
	"context"
	"strings"
	"time"
)

// init asigna el collector real al slot per-OS. Ver software.go.
func init() {
	collectDarwin = realCollectDarwin
}

// realCollectDarwin recolecta paquetes (pkgutil + brew), servicios (launchctl)
// y actualizaciones (softwareupdate -l, sólo si está disponible).
func realCollectDarwin(ctx context.Context, opts *CollectSoftwareOpts) Software {
	const blockTimeout = 5 * time.Second
	var sw Software

	if pkgs := collectDarwinPackages(ctx, opts, blockTimeout); len(pkgs) > 0 {
		sw.Packages = pkgs
	}
	if svcs := collectDarwinServices(ctx, opts, blockTimeout); len(svcs) > 0 {
		sw.Services = svcs
	}
	if ups := collectDarwinUpdates(ctx, opts, blockTimeout); len(ups) > 0 {
		sw.Updates = ups
	}
	return sw
}

func collectDarwinPackages(ctx context.Context, opts *CollectSoftwareOpts, t time.Duration) []Package {
	var out []Package
	if b, err := opts.Sheller.Run(ctx, "pkgutil", []string{"--pkgs"}, t); err == nil {
		sc := bufio.NewScanner(bytes.NewReader(b))
		for sc.Scan() {
			name := strings.TrimSpace(sc.Text())
			if name == "" {
				continue
			}
			out = append(out, Package{Name: name, Source: "pkgutil"})
		}
	}
	if _, err := opts.Sheller.Run(ctx, "sh", []string{"-c", "command -v brew >/dev/null 2>&1"}, 1*time.Second); err == nil {
		if b, err := opts.Sheller.Run(ctx, "brew", []string{"list", "--versions"}, t); err == nil {
			sc := bufio.NewScanner(bytes.NewReader(b))
			for sc.Scan() {
				line := sc.Text()
				parts := strings.Fields(line)
				if len(parts) < 2 {
					continue
				}
				out = append(out, Package{
					Name:    parts[0],
					Version: parts[len(parts)-1],
					Source:  "brew",
				})
			}
		}
	}
	return out
}

func collectDarwinServices(ctx context.Context, opts *CollectSoftwareOpts, t time.Duration) []Service {
	if _, err := opts.Sheller.Run(ctx, "sh", []string{"-c", "command -v launchctl >/dev/null 2>&1"}, 1*time.Second); err != nil {
		return nil
	}
	b, err := opts.Sheller.Run(ctx, "launchctl", []string{"list"}, t)
	if err != nil {
		return nil
	}
	return parseLaunchctlList(b)
}

func collectDarwinUpdates(ctx context.Context, opts *CollectSoftwareOpts, t time.Duration) []Update {
	if _, err := opts.Sheller.Run(ctx, "sh", []string{"-c", "command -v softwareupdate >/dev/null 2>&1"}, 1*time.Second); err != nil {
		return nil
	}
	b, err := opts.Sheller.Run(ctx, "softwareupdate", []string{"-l"}, t)
	if err != nil {
		return nil
	}
	return parseSoftwareUpdate(b)
}

// --- parsers (darwin only) ---

func parseLaunchctlList(b []byte) []Service {
	var out []Service
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		state := "stopped"
		if fields[0] != "-" {
			state = "running"
		}
		out = append(out, Service{Name: fields[2], State: state, Source: "launchd"})
	}
	return out
}

func parseSoftwareUpdate(b []byte) []Update {
	var out []Update
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "*") {
			continue
		}
		parts := strings.SplitN(line[1:], " ", 2)
		if len(parts) < 2 {
			continue
		}
		out = append(out, Update{
			Name:   strings.TrimSpace(parts[0]),
			Source: "softwareupdate",
		})
	}
	return out
}
