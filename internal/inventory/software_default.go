//go:build !linux && !darwin && !windows

package inventory

import "context"

// collectDefault es el fallback para OS no soportados (p.ej. FreeBSD).
// Devuelve un Software vacío; el agent lo registrará en el campo `error`
// del Snapshot si CollectSoftware no encontró nada.
func collectDefault(_ context.Context, _ *CollectSoftwareOpts) Software {
	return Software{}
}
