//go:build windows

package inventory

import (
	"context"
	"testing"
	"time"
)

func TestParseScQueryWindows(t *testing.T) {
	in := `SERVICE_NAME: Spooler
        STATE              : 4  RUNNING
        START_TYPE         : 2  AUTO

SERVICE_NAME: WSearch
        STATE              : 1  STOPPED
        START_TYPE         : 3  DEMAND
`
	svcs := parseScQuery([]byte(in))
	if len(svcs) != 2 {
		t.Fatalf("got %d", len(svcs))
	}
	if svcs[0].Name != "Spooler" || svcs[0].State != "running" {
		t.Fatalf("first: %+v", svcs[0])
	}
}

func TestParseWingetOutput(t *testing.T) {
	in := "Name\tId\tVersion\tAvailable\tSource\n" +
		"Mozilla Firefox\tMozilla.Firefox\t125.0.3\t\twinget\n"
	pkgs := parseWingetList([]byte(in))
	if len(pkgs) != 1 {
		t.Fatalf("got %d", len(pkgs))
	}
	if pkgs[0].Name != "Mozilla Firefox" {
		t.Fatalf("name: %q", pkgs[0].Name)
	}
}

type mockShellerWindows struct{}

func (mockShellerWindows) Run(_ context.Context, _ string, _ []string, _ time.Duration) ([]byte, error) {
	return nil, errNotFoundMock
}

// stub para que ctx no quede unused
var _ = context.Background
