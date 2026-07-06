//go:build darwin

package inventory

import (
	"context"
	"testing"
	"time"
)

func TestCollectDarwinPackagesFromPkgutil(t *testing.T) {
	ms := &mockShellerDarwin{
		responses: map[string][]byte{
			"pkgutil --pkgs": []byte("com.apple.pkg.Gatekeeper\ncom.apple.pkg.Xcode\n"),
		},
		errs: map[string]error{
			"sh -c command -v brew >/dev/null 2>&1": errNotFoundMock,
		},
	}
	opts := &CollectSoftwareOpts{Sheller: ms, OSType: "darwin"}
	pkgs := collectDarwinPackages(context.Background(), opts, time.Second)
	if len(pkgs) != 2 {
		t.Fatalf("got %d pkgs", len(pkgs))
	}
	if pkgs[0].Source != "pkgutil" {
		t.Fatalf("expected pkgutil source, got %q", pkgs[0].Source)
	}
}

func TestParseLaunchctlListLinuxStub(t *testing.T) {
	in := []byte(
		"123\t0\tcom.apple.something\n" +
			"-\t0\torg.sai.agent\n",
	)
	svcs := parseLaunchctlList(in)
	if len(svcs) != 2 {
		t.Fatalf("got %d", len(svcs))
	}
}

type mockShellerDarwin struct {
	responses map[string][]byte
	errs      map[string]error
}

func (m *mockShellerDarwin) Run(_ context.Context, name string, args []string, _ time.Duration) ([]byte, error) {
	key := name + " " + joinArgsLinux(args)
	if e, ok := m.errs[key]; ok {
		return nil, e
	}
	if r, ok := m.responses[key]; ok {
		return r, nil
	}
	return nil, errNotFoundMock
}
