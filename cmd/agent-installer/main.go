// Command agent-installer genera un bundle ZIP pre-configurado del agente
// sin necesidad de tener el server arriba (útil para air-gapped o
// distribución manual).
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Naired01/SAI/internal/bundles"
)

func main() {
	var (
		server   = flag.String("server", "", "URL WSS del server (ej: wss://sai.example.com/api/v1/agent/ws)")
		token    = flag.String("token", "", "Token de enrolamiento")
		out      = flag.String("out", "./sai-bundle.zip", "Path del ZIP a generar")
		osName   = flag.String("os", "", "OS del binario (windows/linux/darwin). Default: GOOS actual")
		arch     = flag.String("arch", "", "Arquitectura (amd64/arm64). Default: GOARCH actual")
		bdir     = flag.String("bundle-dir", "./dist", "Directorio donde están los binarios base")
		insecure = flag.Bool("insecure", false, "Saltar verificación TLS (solo dev)")
	)
	flag.Parse()

	if *server == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "--server y --token son obligatorios")
		flag.Usage()
		os.Exit(2)
	}

	if *osName == "" {
		*osName = runtime.GOOS
	}
	if *arch == "" {
		*arch = runtime.GOARCH
	}

	absBundle, _ := filepath.Abs(*bdir)
	builder := bundles.New(absBundle)

	asset, err := builder.Asset(*osName, *arch)
	if err != nil {
		fmt.Fprintln(os.Stderr, "asset:", err)
		os.Exit(1)
	}

	cfg := bundles.Config{
		ServerURL:       *server,
		EnrollmentToken: *token,
		Labels:          map[string]any{},
		InsecureSkipTLS: *insecure,
	}

	data, err := builder.Build(asset, cfg, asset.OS == "windows")
	if err != nil {
		fmt.Fprintln(os.Stderr, "build:", err)
		os.Exit(1)
	}

	if err := os.WriteFile(*out, data, 0644); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
		os.Exit(1)
	}
	fmt.Printf("Bundle escrito en %s (%d bytes) — OS=%s ARCH=%s\n",
		*out, len(data), asset.OS, asset.Arch)
}