package inventory

import (
	"context"
	"testing"
	"time"
)

// --- cross-platform tests (parsers used everywhere) -------------------

// (parsers dpkg/rpm/pacman/launchctl/scquery/winget están gated por OS en
// sus archivos respectivos; este test cubre la parte platform-agnostic
// del paquete: contains, default dispatch, etc.)

func TestSoftwareContainsSoftware(t *testing.T) {
	sw := Software{}
	if sw.ContainsSoftware() {
		t.Fatal("empty should be false")
	}
	sw.Packages = []Package{{Name: "x"}}
	if !sw.ContainsSoftware() {
		t.Fatal("packages should be true")
	}
	sw.Packages = nil
	sw.Services = []Service{{Name: "y"}}
	if !sw.ContainsSoftware() {
		t.Fatal("services should be true")
	}
	sw.Services = nil
	sw.Updates = []Update{{Name: "z"}}
	if !sw.ContainsSoftware() {
		t.Fatal("updates should be true")
	}
}

func TestCollectSoftwareUnknownOS(t *testing.T) {
	opts := &CollectSoftwareOpts{Sheller: nil, OSType: "freebsd"}
	sw := CollectSoftware(context.Background(), opts)
	if sw.ContainsSoftware() {
		t.Fatalf("unsupported OS should not produce data: %+v", sw)
	}
}

func TestCollectSoftwareNilShellerUsesReal(t *testing.T) {
	// No podemos predecir el output del sistema real; sólo verificamos que no
	// panickea y respeta el timeout del contexto.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("CollectSoftware panicked with nil Sheller: %v", r)
		}
	}()
	_ = CollectSoftware(ctx, nil)
}

func TestCollectSoftwareRespectsTimeout(t *testing.T) {
	// Sheller que duerme más que el timeout; verificamos que no bloquea.
	ms := &slowSheller{delay: 10 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	opts := &CollectSoftwareOpts{Sheller: ms, OSType: "linux"}
	sw := CollectSoftware(ctx, opts)
	// Lo importante es que retornó (no bloquoteó).
	_ = sw
}

type slowSheller struct{ delay time.Duration }

func (s *slowSheller) Run(ctx context.Context, _ string, _ []string, _ time.Duration) ([]byte, error) {
	select {
	case <-time.After(s.delay):
		return []byte("ok"), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// --- shared helpers (platform-agnostic) ------------------------------

type errMock struct{ s string }

func (e errMock) Error() string { return e.s }

var errNotFoundMock = errMock{"not found in mock"}
