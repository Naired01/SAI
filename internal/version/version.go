// Package version expone la metadata de build del servidor y del agente.
//
// Los valores se inyectan en tiempo de compilación con:
//
//	-ldflags "-X github.com/Naired01/SAI/internal/version.Version=v1.2.3 \
//	          -X github.com/Naired01/SAI/internal/version.Commit=abc1234 \
//	          -X github.com/Naired01/SAI/internal/version.BuildTime=2026-07-03T10:00:00Z"
package version

var (
	Version   = "0.1.0-dev"
	Commit    = "unknown"
	BuildTime = "unknown"
	GoVersion = "1.25+"
)

// Info devuelve la metadata lista para serializar.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
}

// Get devuelve la metadata actual.
func Get() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuildTime: BuildTime,
		GoVersion: GoVersion,
	}
}