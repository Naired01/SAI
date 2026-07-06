//go:build windows

package inventory

import (
	"bufio"
	"context"
	"regexp"
	"strings"
	"time"
)

// init asigna el collector real al slot per-OS. Ver software.go.
func init() {
	collectWindows = realCollectWindows
}

// realCollectWindows usa winget (preferido), PowerShell Get-Package, o sc query.
func realCollectWindows(ctx context.Context, opts *CollectSoftwareOpts) Software {
	const blockTimeout = 5 * time.Second
	var sw Software

	if pkgs := collectWindowsPackages(ctx, opts, blockTimeout); len(pkgs) > 0 {
		sw.Packages = pkgs
	}
	if svcs := collectWindowsServices(ctx, opts, blockTimeout); len(svcs) > 0 {
		sw.Services = svcs
	}
	// Updates: requiere módulo PSWindowsUpdate (raro); se deja para Fase 7+.
	return sw
}

func collectWindowsPackages(ctx context.Context, opts *CollectSoftwareOpts, t time.Duration) []Package {
	if hasWinget(opts, t) {
		if b, err := opts.Sheller.Run(ctx, "winget", []string{"list", "--accept-source-agreements"}, t); err == nil {
			return parseWingetList(b)
		}
	}
	if b, err := opts.Sheller.Run(ctx, "powershell",
		[]string{"-NoProfile", "-NonInteractive", "-Command", "Get-Package | Select-Object Name,Version,ProviderName | ConvertTo-Csv -NoTypeInformation"},
		t); err == nil {
		return parseGetPackageCsv(b)
	}
	return nil
}

func hasWinget(opts *CollectSoftwareOpts, t time.Duration) bool {
	_, err := opts.Sheller.Run(context.Background(), "where", []string{"winget"}, t)
	return err == nil
}

func collectWindowsServices(ctx context.Context, opts *CollectSoftwareOpts, t time.Duration) []Service {
	out, err := opts.Sheller.Run(ctx, "sc", []string{"query", "type=", "service"}, t)
	if err != nil {
		return nil
	}
	return parseScQuery(out)
}

// --- parsers (windows only) ---

func parseWingetList(b []byte) []Package {
	var out []Package
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	header := sc.Scan()
	if !header {
		return nil
	}
	cols := strings.Split(sc.Text(), "\t")
	idxName := indexOf(cols, "Name")
	idxVersion := indexOf(cols, "Version")
	idxSource := indexOf(cols, "Source")
	if idxName < 0 {
		return nil
	}
	for sc.Scan() {
		fields := strings.Split(sc.Text(), "\t")
		name := atIdx(fields, idxName)
		if name == "" {
			continue
		}
		p := Package{Name: name}
		if idxVersion >= 0 {
			p.Version = atIdx(fields, idxVersion)
		}
		if idxSource >= 0 {
			p.Source = atIdx(fields, idxSource)
		} else {
			p.Source = "winget"
		}
		out = append(out, p)
	}
	return out
}

func indexOf(cols []string, want string) int {
	for i, c := range cols {
		if strings.EqualFold(strings.TrimSpace(c), want) {
			return i
		}
	}
	return -1
}

func atIdx(parts []string, i int) string {
	if i < 0 || i >= len(parts) {
		return ""
	}
	return strings.TrimSpace(parts[i])
}

func parseGetPackageCsv(b []byte) []Package {
	var out []Package
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	header := sc.Scan()
	if !header {
		return nil
	}
	cols := parseCSVLine(sc.Text())
	idxName := indexOf(cols, "Name")
	idxVer := indexOf(cols, "Version")
	idxProv := indexOf(cols, "ProviderName")
	if idxName < 0 {
		return nil
	}
	for sc.Scan() {
		parts := parseCSVLine(sc.Text())
		name := atIdx(parts, idxName)
		if name == "" {
			continue
		}
		p := Package{Name: name}
		if idxVer >= 0 {
			p.Version = atIdx(parts, idxVer)
		}
		if idxProv >= 0 {
			p.Source = atIdx(parts, idxProv)
		}
		if p.Source == "" {
			p.Source = "psprovider"
		}
		out = append(out, p)
	}
	return out
}

func parseCSVLine(line string) []string {
	var fields []string
	var cur strings.Builder
	inQuotes := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case ch == '"' && inQuotes && i+1 < len(line) && line[i+1] == '"':
			cur.WriteByte('"')
			i++
		case ch == '"':
			inQuotes = !inQuotes
		case ch == ',' && !inQuotes:
			fields = append(fields, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(ch)
		}
	}
	fields = append(fields, cur.String())
	return fields
}

var (
	scNameRe  = regexp.MustCompile(`(?i)SERVICE_NAME:\s*(\S+)`)
	scStateRe = regexp.MustCompile(`(?i)STATE\s+:\s+\d+\s+(\w+)`)
	scStartRe = regexp.MustCompile(`(?i)START_TYPE\s+:\s+\d+\s+(\w+)`)
)

func parseScQuery(b []byte) []Service {
	var out []Service
	var cur Service
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		line := sc.Text()
		if m := scNameRe.FindStringSubmatch(line); m != nil {
			if cur.Name != "" {
				out = append(out, cur)
			}
			cur = Service{Name: m[1], Source: "scm"}
			continue
		}
		if m := scStateRe.FindStringSubmatch(line); m != nil {
			cur.State = strings.ToLower(m[1])
		}
		if m := scStartRe.FindStringSubmatch(line); m != nil {
			cur.StartType = strings.ToLower(m[1])
		}
	}
	if cur.Name != "" {
		out = append(out, cur)
	}
	return out
}
