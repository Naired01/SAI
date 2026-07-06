package inventory

import "runtime"

// runtimeGOOS devuelve el runtime.GOOS. Existe en una función para que
// los tests + per-OS collectors puedan override via init() cuando sea
// necesario (p.ej. tests que simulan un Windows en un Linux CI).
func runtimeGOOS() string {
	return runtime.GOOS
}

// runtimeGOARCH devuelve el runtime.GOARCH.
func runtimeGOARCH() string {
	return runtime.GOARCH
}
