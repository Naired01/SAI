package inventory

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCollectWithAllOK(t *testing.T) {
	opts := CollectorOpts{
		Host: func(ctx context.Context) (Host, error) {
			return Host{Hostname: "test-host", OS: "linux", UptimeSecs: 100}, nil
		},
		CPU: func(ctx context.Context) ([]CPUSlot, error) {
			return []CPUSlot{{ModelName: "Test CPU", Cores: 4}}, nil
		},
		Memory: func(ctx context.Context) (Memory, error) {
			return Memory{TotalBytes: 8 * 1024 * 1024 * 1024, AvailableByte: 4 * 1024 * 1024 * 1024}, nil
		},
		Disks: func(ctx context.Context) ([]Disk, error) {
			return []Disk{{Device: "/dev/sda1", Mountpoint: "/", FSType: "ext4", TotalBytes: 100 * 1024 * 1024 * 1024}}, nil
		},
		Network: func(ctx context.Context) ([]NetIface, error) {
			return []NetIface{{Name: "eth0", HardwareAddr: "aa:bb", Addrs: []string{"10.0.0.1/24"}, State: "up"}}, nil
		},
	}
	snap := collectWith(context.Background(), opts, "0.2.0-test")
	if snap.Error != "" {
		t.Fatalf("unexpected Error: %q", snap.Error)
	}
	if snap.Hardware.Host.Hostname != "test-host" {
		t.Fatalf("host roundtrip: %+v", snap.Hardware.Host)
	}
	if len(snap.Hardware.CPU) != 1 {
		t.Fatalf("cpu: %+v", snap.Hardware.CPU)
	}
	if snap.Hardware.Memory.TotalBytes == 0 {
		t.Fatalf("mem empty: %+v", snap.Hardware.Memory)
	}
	if len(snap.Hardware.Disks) != 1 {
		t.Fatalf("disks: %+v", snap.Hardware.Disks)
	}
	if len(snap.Hardware.Network) != 1 {
		t.Fatalf("network: %+v", snap.Hardware.Network)
	}
	if snap.SchemaVer != SchemaVer {
		t.Fatalf("SchemaVer: %d", snap.SchemaVer)
	}
	if snap.AgentVersion != "0.2.0-test" {
		t.Fatalf("AgentVersion: %q", snap.AgentVersion)
	}
	if snap.CollectedAt.IsZero() {
		t.Fatal("CollectedAt not set")
	}
}

func TestCollectWithErrors(t *testing.T) {
	opts := CollectorOpts{
		Host:    func(ctx context.Context) (Host, error) { return Host{}, errors.New("host boom") },
		CPU:     func(ctx context.Context) ([]CPUSlot, error) { return nil, errors.New("cpu boom") },
		Memory:  func(ctx context.Context) (Memory, error) { return Memory{}, errors.New("mem boom") },
		Disks:   func(ctx context.Context) ([]Disk, error) { return nil, errors.New("disk boom") },
		Network: func(ctx context.Context) ([]NetIface, error) { return nil, errors.New("net boom") },
	}
	snap := collectWith(context.Background(), opts, "0.2.0")
	if snap.Error == "" {
		t.Fatal("expected Error to be populated when all collectors fail")
	}
	for _, want := range []string{"host:", "cpu:", "mem:", "disk:", "net:"} {
		if !contains(snap.Error, want) {
			t.Fatalf("Error %q missing %q", snap.Error, want)
		}
	}
}

func TestCollectWithContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelar antes de invocar
	opts := CollectorOpts{
		Host: func(c context.Context) (Host, error) {
			if err := c.Err(); err != nil {
				return Host{}, err
			}
			return Host{}, nil
		},
		CPU:     func(c context.Context) ([]CPUSlot, error) { return nil, c.Err() },
		Memory:  func(c context.Context) (Memory, error) { return Memory{}, c.Err() },
		Disks:   func(c context.Context) ([]Disk, error) { return nil, c.Err() },
		Network: func(c context.Context) ([]NetIface, error) { return nil, c.Err() },
	}
	snap := collectWith(ctx, opts, "0.2.0")
	if snap.Error == "" {
		t.Fatal("expected Error to reflect ctx cancellation")
	}
}

func TestCollectRealRun(t *testing.T) {
	// Smoke real: corre Collect con timeout corto. Si falla (p.ej. en un CI
	// sin /proc accesible), es OK — sólo loggeamos. Sirve para detectar
	// regresiones de import cuando gopsutil cambia de API.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	snap := Collect(ctx, "test")
	if snap.CollectedAt.IsZero() {
		t.Fatal("CollectedAt not set in real Collect()")
	}
	// No assert sobre contenido (depende de host de test); basta con que
	// el collector regrese sin panic.
	_ = snap
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
