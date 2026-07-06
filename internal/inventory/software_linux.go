//go:build linux

package inventory

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"
)

// init asigna el collector real al slot per-OS. Ver software.go.
func init() {
	collectLinux = realCollectLinux
}

// realCollectLinux ejecuta los 3 bloques con primera-que-funcione:
//
//   * Packages: dpkg-query → rpm -qa → pacman -Q
//   * Services: systemctl list-units
//   * Updates:  apt list --upgradable → yum check-update
//
// Cada comando corre con 5s timeout duro. Si uno falla o no existe, se
// prueba el siguiente. Si ninguno funciona, el bloque queda ausente.
func realCollectLinux(ctx context.Context, opts *CollectSoftwareOpts) Software {
	const blockTimeout = 5 * time.Second
	var sw Software

	if pkgs, src, err := collectLinuxPackages(ctx, opts, blockTimeout); err == nil && len(pkgs) > 0 {
		for i := range pkgs {
			pkgs[i].Source = src
		}
		sw.Packages = pkgs
	}
	if svcs, src, err := collectLinuxServices(ctx, opts, blockTimeout); err == nil && len(svcs) > 0 {
		for i := range svcs {
			svcs[i].Source = src
		}
		sw.Services = svcs
	}
	if ups, src, err := collectLinuxUpdates(ctx, opts, blockTimeout); err == nil && len(ups) > 0 {
		for i := range ups {
			ups[i].Source = src
		}
		sw.Updates = ups
	}
	return sw
}

func collectLinuxPackages(ctx context.Context, opts *CollectSoftwareOpts, t time.Duration) ([]Package, string, error) {
	type attempt struct {
		source string
		probe  string
		cmd    string
		args   []string
		parse  func(b []byte) []Package
	}
	attempts := []attempt{
		{"dpkg", "dpkg-query", "dpkg-query", []string{"-W", "-f=${Package}\t${Version}\n"}, parseDpkg},
		{"rpm", "rpm", "rpm", []string{"-qa", "--queryformat", "%{NAME}\t%{VERSION}\n"}, parseRpm},
		{"pacman", "pacman", "pacman", []string{"-Q"}, parsePacman},
	}
	for _, a := range attempts {
		if _, err := opts.Sheller.Run(ctx, "sh", []string{"-c", "command -v " + a.probe + " >/dev/null 2>&1"}, 1*time.Second); err != nil {
			continue
		}
		out, err := opts.Sheller.Run(ctx, a.cmd, a.args, t)
		if err != nil {
			continue
		}
		return a.parse(out), a.source, nil
	}
	return nil, "", fmt.Errorf("no package manager found")
}

func collectLinuxServices(ctx context.Context, opts *CollectSoftwareOpts, t time.Duration) ([]Service, string, error) {
	if _, err := opts.Sheller.Run(ctx, "sh", []string{"-c", "command -v systemctl >/dev/null 2>&1"}, 1*time.Second); err != nil {
		return nil, "", err
	}
	out, err := opts.Sheller.Run(ctx, "systemctl",
		[]string{"list-units", "--type=service", "--no-legend", "--no-pager"},
		t)
	if err != nil {
		return nil, "", err
	}
	return parseSystemctlUnits(out), "systemd", nil
}

func collectLinuxUpdates(ctx context.Context, opts *CollectSoftwareOpts, t time.Duration) ([]Update, string, error) {
	type attempt struct {
		source string
		probe  string
		cmd    string
		args   []string
		parse  func(b []byte) []Update
	}
	attempts := []attempt{
		{"apt", "apt", "apt", []string{"list", "--upgradable"}, parseAptList},
		{"yum", "yum", "yum", []string{"check-update", "--quiet"}, parseYumCheckUpdate},
	}
	for _, a := range attempts {
		if _, err := opts.Sheller.Run(ctx, "sh", []string{"-c", "command -v " + a.probe + " >/dev/null 2>&1"}, 1*time.Second); err != nil {
			continue
		}
		out, err := opts.Sheller.Run(ctx, a.cmd, a.args, t)
		if err != nil {
			if a.source == "yum" && len(out) > 0 {
				out = stripYumFirstLine(out)
				if len(bytes.TrimSpace(out)) > 0 {
					return a.parse(out), a.source, nil
				}
			}
			continue
		}
		return a.parse(out), a.source, nil
	}
	return nil, "", fmt.Errorf("no updater found")
}

// --- parsers (linux only; probados por tests en software_linux_test.go) ---

func parseDpkg(b []byte) []Package {
	var out []Package
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := sc.Text()
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 || parts[0] == "" {
			continue
		}
		out = append(out, Package{Name: parts[0], Version: parts[1]})
	}
	return out
}

func parseRpm(b []byte) []Package {
	var out []Package
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := sc.Text()
		idx := strings.Index(line, "\t")
		if idx < 0 {
			continue
		}
		out = append(out, Package{Name: line[:idx], Version: line[idx+1:]})
	}
	return out
}

func parsePacman(b []byte) []Package {
	var out []Package
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		parts := strings.SplitN(sc.Text(), " ", 2)
		if len(parts) < 2 {
			continue
		}
		out = append(out, Package{Name: parts[0], Version: parts[1]})
	}
	return out
}

func parseSystemctlUnits(b []byte) []Service {
	var out []Service
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := sc.Text()
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		name := fields[0]
		state := "stopped"
		if fields[3] == "running" {
			state = "running"
		}
		out = append(out, Service{Name: name, State: state})
	}
	return out
}

func parseAptList(b []byte) []Update {
	var out []Update
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "Listing...") {
			continue
		}
		idx := strings.Index(line, "/")
		eq := strings.Index(line, " ")
		if idx < 0 || eq <= idx {
			continue
		}
		name := line[:idx]
		rest := line[eq+1:]
		parts := strings.SplitN(rest, " ", 2)
		if len(parts) < 1 {
			continue
		}
		out = append(out, Update{Name: name, AvailableVersion: parts[0]})
	}
	return out
}

func parseYumCheckUpdate(b []byte) []Update {
	var out []Update
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		nameVer := strings.SplitN(fields[0], ".", 2)
		if len(nameVer) == 0 {
			continue
		}
		out = append(out, Update{Name: nameVer[0], AvailableVersion: fields[1]})
	}
	return out
}

func stripYumFirstLine(b []byte) []byte {
	idx := bytes.Index(b, []byte("\n"))
	if idx < 0 {
		return b
	}
	return b[idx+1:]
}
