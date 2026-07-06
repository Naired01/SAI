// Package inventory implementa el modelo, el almacenamiento y los mensajes WS
// del inventario de hardware/software (Fase 2).
//
// Esquema de tablas (ver internal/db/sql/0003_inventory.sql):
//
//   * agent_inventory    — 1 fila por agente, UPSERT en cada snapshot.
//   * inventory_snapshots — append-only BIGSERIAL, historial paginado.
//   * inventory_events    — log del flujo (requested/received/failed/stale).
//
// Mensajes WS (ver PLAN.md §5):
//
//   server → agent: { "type":"inventory_request", "id":"<uuid>", "include":["hardware"] }
//   agent  → server: { "type":"inventory_snapshot", "id":"<uuid>",
//                       "schema_ver":1, "agent_version":"0.2.0",
//                       "hardware":{...}, "software":{...} }
package inventory

import (
	"time"
)

// SchemaVer es la versión del shape del payload hardware/software. Cuando
// cambiemos campos incompatibles, bumpear y aceptar ambas en Validate().
//
// Historial:
//   * 1 — Fase 2.0: solo bloque hardware (Host/CPU/Memory/Disks/Network).
//   * 2 — Fase 2.1: añade software {Packages, Services, Updates}. El bloque
//                    hardware es opcional en v2 (agentes sin permisos HW
//                    igual pueden reportar inventario de paquetes).
const SchemaVer = 2

// SchemaVersionsAccepted lista los schemas que el server acepta. Al bump,
// añadir la nueva aquí y mantener la anterior para back-compat con agentes
// ya desplegados.
var SchemaVersionsAccepted = []int{1, 2}

// DefaultTTL define cuándo un inventario se considera "stale". Un agente
// sin snapshot en DefaultTTL recibe un inventory_request al welcome.
const DefaultTTL = 24 * time.Hour

// MaxSnapshotsPerAgent limita el historial por agente. La purga corre cada
// hora en el server (cmd/server/main.go).
const MaxSnapshotsPerAgent = 100

// -----------------------------------------------------------------------------
// Modelo de datos (lo que viaja por WS y se persiste como JSONB)
// -----------------------------------------------------------------------------

// Host agrupa información de plataforma: hostname, OS, kernel, uptime.
type Host struct {
	Hostname   string `json:"hostname"`
	OS         string `json:"os"`            // runtime.GOOS
	Platform   string `json:"platform"`      // "linux", "windows", "darwin"
	KernelArch string `json:"kernel_arch"`   // amd64 / arm64
	KernelVer  string `json:"kernel_ver,omitempty"`
	UptimeSecs uint64 `json:"uptime_secs"`
	BootTime   string `json:"boot_time,omitempty"` // ISO8601 si está disponible
}

// CPUSlot describe un procesador lógico. Sistemas multiprocesador pueden
// tener más de uno.
type CPUSlot struct {
	ModelName string  `json:"model_name"`
	Vendor    string  `json:"vendor,omitempty"`
	Family    string  `json:"family,omitempty"`
	Model     string  `json:"model,omitempty"`
	Cores     int32   `json:"cores"`
	MHz       float64 `json:"mhz"`
}

// Memory resume la memoria del sistema en bytes.
type Memory struct {
	TotalBytes    uint64 `json:"total_bytes"`
	AvailableByte uint64 `json:"available_bytes"`
	UsedBytes     uint64 `json:"used_bytes"`
}

// Disk describe una partición montada. El label es opcional (Windows).
type Disk struct {
	Device     string `json:"device"`
	Mountpoint string `json:"mountpoint"`
	FSType     string `json:"fs_type"`
	TotalBytes uint64 `json:"total_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
	Label      string `json:"label,omitempty"`
}

// NetIface describe una interfaz de red. Addrs incluye IPv4 + IPv6.
type NetIface struct {
	Name        string   `json:"name"`
	HardwareAddr string  `json:"hardware_addr,omitempty"`
	MTU         int      `json:"mtu,omitempty"`
	Flags       string   `json:"flags,omitempty"`
	Addrs       []string `json:"addrs,omitempty"`
	State       string   `json:"state,omitempty"` // "up" / "down"
}

// Hardware es el bloque {host,cpu,memory,disks,network} del snapshot.
// SchemaVer y AgentVersion se copian también al nivel superior (Snapshot).
type Hardware struct {
	Host    Host       `json:"host"`
	CPU     []CPUSlot  `json:"cpu"`
	Memory  Memory     `json:"memory"`
	Disks   []Disk     `json:"disks"`
	Network []NetIface `json:"network"`
}

// Package representa un paquete instalado. La fuente identifica el mecanismo
// de instalación: dpkg (Debian/Ubuntu), rpm (RHEL/Fedora/openSUSE), pacman
// (Arch), winget (Windows), choco (Windows), brew (macOS), pkgutil (macOS).
type Package struct {
	Name      string `json:"name"`                // "firefox"
	Version   string `json:"version"`             // "125.0.3"
	Source    string `json:"source,omitempty"`   // "dpkg" / "rpm" / "brew" / ...
	Publisher string `json:"publisher,omitempty"` // "Mozilla Foundation"
}

// Service representa un servicio del SO con su estado y tipo de inicio.
// Source identifica el mecanismo: systemd (Linux), launchd (macOS), SCM (Windows).
type Service struct {
	Name      string `json:"name"`
	State     string `json:"state"`               // "running" | "stopped" | "unknown"
	StartType string `json:"start_type,omitempty"` // "auto" | "manual" | "disabled"
	Source    string `json:"source,omitempty"`   // "systemd" / "launchd" / "scm"
}

// Update representa una actualización disponible pendiente de aplicar.
type Update struct {
	Name             string `json:"name"`
	CurrentVersion   string `json:"current_version,omitempty"`
	AvailableVersion string `json:"available_version,omitempty"`
	Severity         string `json:"severity,omitempty"` // "security" | "bugfix" | "feature"
	Source           string `json:"source,omitempty"`   // "apt" / "yum" / "winget" / ...
}

// Software agrupa los datos no-HW del sistema. Se omite cada bloque si el
// collector no pudo obtener datos (permisos, OS no soportado, timeout).
type Software struct {
	Packages []Package `json:"packages,omitempty"`
	Services []Service `json:"services,omitempty"`
	Updates  []Update  `json:"updates,omitempty"`
}

// ContainsSoftware devuelve true si hay al menos un bloque con datos útiles.
// Sirve al panel para mostrar "Sin inventario de software" sin distinguer.
func (s Software) ContainsSoftware() bool {
	return len(s.Packages) > 0 || len(s.Services) > 0 || len(s.Updates) > 0
}

// Snapshot es el payload completo que el agente envía. Se persiste tal cual
// en agent_inventory (UPSERT) y en inventory_snapshots (append).
type Snapshot struct {
	SchemaVer    int       `json:"schema_ver"`
	AgentVersion string    `json:"agent_version"`
	Hardware     Hardware  `json:"hardware"`
	Software     Software  `json:"software"`
	Error        string    `json:"error,omitempty"` // si Collect falló parcialmente
	CollectedAt  time.Time `json:"collected_at"`    // hora del agente al recolectar
}

// -----------------------------------------------------------------------------
// Mensajes WS
// -----------------------------------------------------------------------------

// ReqMsg es server → agent. El agent debe responder con SnapshotMsg que
// contenga el mismo ID.
type ReqMsg struct {
	Type    string   `json:"type"` // siempre "inventory_request"
	ID      string   `json:"id"`   // UUID v4 (server-side)
	Include []string `json:"include,omitempty"`
}

// SnapshotMsg es agent → server. El ID debe coincidir con la ReqMsg original.
type SnapshotMsg struct {
	Type         string   `json:"type"` // siempre "inventory_snapshot"
	ID           string   `json:"id"`
	SchemaVer    int      `json:"schema_ver"`
	AgentVersion string   `json:"agent_version"`
	Hardware     *Hardware `json:"hardware"`
	Software     *Software `json:"software"`
	Error        string   `json:"error,omitempty"`
	CollectedAt  time.Time `json:"collected_at"`
}

// Validate hace un shape-check. Acepta los schemas en `SchemaVersionsAccepted`
// para tolerar back-compat con agentes que aún envíen la versión anterior.
func (s *SnapshotMsg) Validate() error {
	if s.ID == "" {
		return ErrEmptyCorrelationID
	}
	if !isSupportedSchema(s.SchemaVer) {
		return ErrUnsupportedSchema
	}
	// En schema 1 (legacy Fase 2.0) el bloque hardware es obligatorio.
	// En schema 2 (Fase 2.1) es opcional — puede venir solo software.
	if s.SchemaVer == 1 && s.Hardware == nil {
		return ErrMissingHardware
	}
	return nil
}

func isSupportedSchema(v int) bool {
	for _, x := range SchemaVersionsAccepted {
		if x == v {
			return true
		}
	}
	return false
}
