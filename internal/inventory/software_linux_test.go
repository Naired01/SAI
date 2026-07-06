//go:build linux

package inventory

import (
	"context"
	"testing"
	"time"
)

func TestCollectLinuxPackagesHappyPath(t *testing.T) {
	ms := &mockShellerLinux{
		responses: map[string][]byte{
			"sh -c command -v dpkg-query ":                 []byte("ok"),
			"dpkg-query -W -f=${Package}\\t${Version}\\n": []byte("curl\t7.85.0\ngit\t2.42.0\n"),
		},
	}
	opts := &CollectSoftwareOpts{Sheller: ms, OSType: "linux"}
	pkgs, src, err := collectLinuxPackages(context.Background(), opts, time.Second)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if src != "dpkg" {
		t.Fatalf("source: got %q want dpkg", src)
	}
	if len(pkgs) != 2 {
		t.Fatalf("got %d packages", len(pkgs))
	}
}

func TestCollectLinuxNoPackageManager(t *testing.T) {
	ms := &mockShellerLinux{
		errs: map[string]error{
			"sh -c command -v dpkg-query ": errNotFoundMock,
			"sh -c command -v rpm ":       errNotFoundMock,
			"sh -c command -v pacman ":    errNotFoundMock,
		},
	}
	opts := &CollectSoftwareOpts{Sheller: ms, OSType: "linux"}
	_, _, err := collectLinuxPackages(context.Background(), opts, time.Second)
	if err == nil {
		t.Fatal("expected error when no package manager present")
	}
}

func TestParseDpkgLines(t *testing.T) {
	in := []byte("firefox\t125.0.3-1\nvim\t9.0.1000-1\n")
	pkgs := parseDpkg(in)
	if len(pkgs) != 2 || pkgs[0].Name != "firefox" {
		t.Fatalf("parseDpkg: %+v", pkgs)
	}
}

func TestParseSystemctlUnitsLinux(t *testing.T) {
	in := []byte(
		"sshd.service loaded active running OpenBSD Secure Shell\n" +
			"docker.service loaded inactive dead Container engine\n",
	)
	svcs := parseSystemctlUnits(in)
	if len(svcs) != 2 {
		t.Fatalf("got %d", len(svcs))
	}
	if svcs[0].State != "running" || svcs[1].State != "stopped" {
		t.Fatalf("states: %s / %s", svcs[0].State, svcs[1].State)
	}
}

// --- linux MockSheller con respuesta incluso en error (yum) ---

type mockShellerLinux struct {
	responses map[string][]byte
	errs      map[string]error
	outForErr map[string][]byte
}

func (m *mockShellerLinux) Run(_ context.Context, name string, args []string, _ time.Duration) ([]byte, error) {
	key := name + " " + joinArgsLinux(args)
	if e, ok := m.errs[key]; ok {
		if out, ok := m.outForErr[key]; ok {
			return out, e
		}
		return nil, e
	}
	if r, ok := m.responses[key]; ok {
		return r, nil
	}
	return nil, errNotFoundMock
}

func joinArgsLinux(args []string) string {
	out := ""
	for _, a := range args {
		out += " " + a
	}
	return out
}
