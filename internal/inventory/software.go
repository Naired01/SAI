package inventory

import (
	"context"
	"os/exec"
	"time"
)

// Sheller abstrae la ejecución de subprocesos para que los unit tests puedan
// inyectar respuestas predefinidas sin tocar el sistema real. La firma es
// mínima: comando + args + timeout. El collector parsea lo que devuelva.
type Sheller interface {
	Run(ctx context.Context, name string, args []string, timeout time.Duration) ([]byte, error)
}

// RealSheller es la implementación por defecto: usa os/exec.
type RealSheller struct{}

// Run ejecuta el comando con timeout duro. Si el timeout vence, mata el
// proceso y devuelve error.
func (RealSheller) Run(ctx context.Context, name string, args []string, timeout time.Duration) ([]byte, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, name, args...) //nolint:gosec // comandos curados
	return cmd.Output()
}

// CollectSoftwareOpts permite inyectar dependencias en CollectSoftware. Si es
// nil, se usan las defaults (RealSheller + OS actual).
type CollectSoftwareOpts struct {
	Sheller Sheller
	OSType  string // "linux" | "darwin" | "windows" | "" (runtime.GOOS)
}

// collectLinux / collectDarwin / collectWindows se inicializan por init()
// en cada archivo per-OS (build tags). Si no hay init del OS actual (p.ej.
// FreeBSD), quedan como noop.
var (
	collectLinux  = noopCollector
	collectDarwin = noopCollector
	collectWindows = noopCollector
)

func noopCollector(_ context.Context, _ *CollectSoftwareOpts) Software {
	return Software{}
}

// CollectSoftware agrupa los datos de software del equipo. Cada bloque se
// obtiene con un timeout corto (5s) y se salta si falla; nunca aborta la
// recolección entera. Devuelve un Software con los bloques que pudo poblar.
func CollectSoftware(ctx context.Context, opts *CollectSoftwareOpts) Software {
	if opts == nil {
		opts = &CollectSoftwareOpts{}
	}
	if opts.Sheller == nil {
		opts.Sheller = RealSheller{}
	}
	if opts.OSType == "" {
		opts.OSType = runtimeGOOS()
	}
	switch opts.OSType {
	case "linux":
		return collectLinux(ctx, opts)
	case "darwin":
		return collectDarwin(ctx, opts)
	case "windows":
		return collectWindows(ctx, opts)
	default:
		return Software{}
	}
}
