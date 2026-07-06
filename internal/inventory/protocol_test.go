package inventory

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestSnapshotMsgValidateAcceptsV1(t *testing.T) {
	hw := &Hardware{Host: Host{Hostname: "h1"}, Memory: Memory{TotalBytes: 1}}
	sw := &Software{}
	s := &SnapshotMsg{
		Type:         "inventory_snapshot",
		ID:           "abc-123",
		SchemaVer:    1,
		AgentVersion: "0.2.0",
		Hardware:     hw,
		Software:     sw,
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("Validate should accept v1 + hardware present: %v", err)
	}
}

func TestSnapshotMsgValidateRejectsEmptyID(t *testing.T) {
	s := &SnapshotMsg{
		SchemaVer: 1, AgentVersion: "0.2.0",
		Hardware: &Hardware{}, Software: &Software{},
	}
	if err := s.Validate(); !errors.Is(err, ErrEmptyCorrelationID) {
		t.Fatalf("expected ErrEmptyCorrelationID, got %v", err)
	}
}

func TestSnapshotMsgValidateRejectsUnsupportedSchema(t *testing.T) {
	s := &SnapshotMsg{
		ID:        "x",
		SchemaVer: 999,
		Hardware:  &Hardware{}, Software: &Software{},
	}
	if err := s.Validate(); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("expected ErrUnsupportedSchema, got %v", err)
	}
}

func TestSnapshotMsgValidateRejectsMissingHardware(t *testing.T) {
	s := &SnapshotMsg{
		ID:        "x",
		SchemaVer: 1,
		Hardware:  nil,
		Software:  &Software{},
	}
	if err := s.Validate(); !errors.Is(err, ErrMissingHardware) {
		t.Fatalf("expected ErrMissingHardware, got %v", err)
	}
}

func TestSnapshotMsgValidateAcceptsV2WithoutHardware(t *testing.T) {
	// Schema 2 permite snapshots sólo-software (HW sin permisos,
	// p.ej. agentes en servidores muy restringidos).
	s := &SnapshotMsg{
		ID:        "x",
		SchemaVer: 2,
		Hardware:  nil,
		Software:  &Software{Packages: []Package{{Name: "vim"}}},
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("v2 without hardware should be valid: %v", err)
	}
}

func TestSnapshotMsgValidateAcceptsV2WithBothBlocks(t *testing.T) {
	s := &SnapshotMsg{
		ID:        "x",
		SchemaVer: 2,
		Hardware:  &Hardware{Host: Host{Hostname: "h"}},
		Software:  &Software{Packages: []Package{{Name: "vim"}}},
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("v2 with both should be valid: %v", err)
	}
}

func TestSnapshotMsgValidateAcceptsV1WithHardware(t *testing.T) {
	s := &SnapshotMsg{
		ID:        "x",
		SchemaVer: 1,
		Hardware:  &Hardware{Host: Host{Hostname: "h"}},
		Software:  nil, // v1 no requiere software
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("v1 with hardware should be valid: %v", err)
	}
}

func TestSnapshotJSONRoundtrip(t *testing.T) {
	original := Snapshot{
		SchemaVer:    1,
		AgentVersion: "0.2.0",
		CollectedAt:  time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC),
		Hardware: Hardware{
			Host:   Host{Hostname: "lab-01", OS: "linux", Platform: "linux", KernelArch: "amd64", UptimeSecs: 12345},
			CPU:    []CPUSlot{{ModelName: "AMD Ryzen 7", Cores: 16, MHz: 3800}},
			Memory: Memory{TotalBytes: 32 * 1024 * 1024 * 1024, UsedBytes: 12 * 1024 * 1024 * 1024},
			Disks:  []Disk{{Device: "/dev/sda1", Mountpoint: "/", FSType: "ext4", TotalBytes: 500 * 1024 * 1024 * 1024}},
			Network: []NetIface{
				{Name: "eth0", HardwareAddr: "aa:bb:cc:dd:ee:ff", MTU: 1500, Addrs: []string{"10.0.0.5/24", "fe80::1/64"}},
			},
		},
		Software: Software{},
	}
	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded Snapshot
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Hardware.Host.Hostname != "lab-01" {
		t.Fatalf("hostname roundtrip: got %q", decoded.Hardware.Host.Hostname)
	}
	if len(decoded.Hardware.CPU) != 1 || decoded.Hardware.CPU[0].ModelName != "AMD Ryzen 7" {
		t.Fatalf("cpu roundtrip: %+v", decoded.Hardware.CPU)
	}
	if !decoded.CollectedAt.Equal(original.CollectedAt) {
		t.Fatalf("collected_at roundtrip mismatch: got %v want %v", decoded.CollectedAt, original.CollectedAt)
	}
}

func TestReqMsgJSONRoundtripAndDiscriminator(t *testing.T) {
	r := ReqMsg{Type: "inventory_request", ID: "id-1", Include: []string{"hardware"}}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["type"] != "inventory_request" {
		t.Fatalf("type discriminator lost: %v", raw["type"])
	}
	var back ReqMsg
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.ID != "id-1" || len(back.Include) != 1 || back.Include[0] != "hardware" {
		t.Fatalf("ReqMsg roundtrip lost data: %+v", back)
	}
}

func TestSnapshotMsgJSONRoundtrip(t *testing.T) {
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	s := SnapshotMsg{
		Type:         "inventory_snapshot",
		ID:           "id-1",
		SchemaVer:    1,
		AgentVersion: "0.2.0",
		Hardware:     &Hardware{Host: Host{Hostname: "h"}},
		Software:     &Software{},
		CollectedAt:  now,
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var back SnapshotMsg
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Type != "inventory_snapshot" || back.ID != "id-1" || back.SchemaVer != 1 {
		t.Fatalf("discriminator lost: %+v", back)
	}
	if !back.CollectedAt.Equal(now) {
		t.Fatalf("collected_at lost: got %v want %v", back.CollectedAt, now)
	}
}
