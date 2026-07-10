// Package bundles ensambla el ZIP pre-configurado del agente (binario
// base + config.json + script de instalación) que el admin descarga
// desde el panel.
//
// Los binarios base se leen de cfg.BundleDir (variable SAI_BUNDLE_DIR).
// El CI los coloca ahí al deployar la imagen Docker o al extraer el
// artefacto de GitHub Releases.
package bundles

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Target representa el OS/arch del binario a servir.
type Target struct {
	OS   string // "windows" | "linux" | "darwin"
	Arch string // "amd64" | "arm64"
}

// Asset describe el ZIP que el agente debe instalar (config + install script + binario).
type Asset struct {
	BinaryPath string // path local al binario base
	Filename   string // nombre dentro del ZIP
	OS         string
	Arch       string
}

// Builder ensambla el ZIP para un token.
type Builder struct {
	BundleDir string // donde están los binarios base
}

// New devuelve un builder.
func New(bundleDir string) *Builder {
	return &Builder{BundleDir: bundleDir}
}

// Asset devuelve el asset para el (os, arch) solicitado.
// Si os o arch están vacíos devuelve error: el caller debe pasar
// ambos explícitamente (no se hace fallback a runtime.GOOS para
// evitar servir el binario incorrecto cuando el server corre en
// Docker Linux pero el admin quiere un bundle para Windows/macOS).
func (b *Builder) Asset(osName, arch string) (*Asset, error) {
	osName = normalizeOS(osName)
	arch = normalizeArch(arch)
	if osName == "" || arch == "" {
		return nil, fmt.Errorf("os and arch query params are required (e.g. ?os=windows&arch=amd64)")
	}
	if !validOS(osName) {
		return nil, fmt.Errorf("unsupported OS: %s (supported: windows, linux, darwin)", osName)
	}
	if !validArch(arch) {
		return nil, fmt.Errorf("unsupported arch: %s (supported: amd64, arm64)", arch)
	}

	binName := fmt.Sprintf("sai-agent-%s-%s", osName, arch)
	if osName == "windows" {
		binName += ".exe"
	}
	full := filepath.Join(b.BundleDir, binName)
	if _, err := os.Stat(full); err != nil {
		return nil, fmt.Errorf("agent binary not found: %s (rebuild Docker image or run scripts/build-release.sh)", full)
	}
	return &Asset{
		BinaryPath: full,
		Filename:   binName,
		OS:         osName,
		Arch:       arch,
	}, nil
}

// Config es el JSON que el agente lee al arrancar.
type Config struct {
	ServerURL        string         `json:"server_url"`
	EnrollmentToken  string         `json:"enrollment_token"`
	AgentID          string         `json:"agent_id"`
	Labels           map[string]any `json:"labels,omitempty"`
	InsecureSkipTLS  bool           `json:"insecure_skip_tls,omitempty"`
}

// Build ensambla el ZIP y devuelve los bytes listos para enviar al cliente.
func (b *Builder) Build(a *Asset, cfg Config, platformWindows bool) ([]byte, error) {
	if a == nil {
		return nil, errors.New("nil asset")
	}
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)

	// 1) Binario base
	if err := addFile(zw, a.Filename, a.BinaryPath); err != nil {
		return nil, err
	}

	// 2) config.json
	cfgBytes, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := addBytes(zw, "config.json", cfgBytes, 0644); err != nil {
		return nil, err
	}

	// 3) install script
	if platformWindows {
		if err := addBytes(zw, "install.ps1",
			[]byte(renderInstallWindows(a.Filename, cfg.ServerURL)), 0755); err != nil {
			return nil, err
		}
	} else {
		if err := addBytes(zw, "install.sh",
			[]byte(renderInstallUnix(a.Filename, cfg.ServerURL, a.OS)), 0755); err != nil {
			return nil, err
		}
	}

	// 4) README
	readme := renderReadme(a.Filename, platformWindows)
	if err := addBytes(zw, "README.txt", []byte(readme), 0644); err != nil {
		return nil, err
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func addFile(zw *zip.Writer, name, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = name
	hdr.Method = zip.Deflate
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

func addBytes(zw *zip.Writer, name string, data []byte, mode os.FileMode) error {
	hdr := &zip.FileHeader{
		Name:   name,
		Method: zip.Deflate,
	}
	hdr.SetMode(mode)
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// -----------------------------------------------------------------------------
// Scripts de instalación
// -----------------------------------------------------------------------------

func renderInstallWindows(binaryName, serverURL string) string {
	return fmt.Sprintf("# SAI Agent installer for Windows\n"+
		"# Server: %s\n"+
		"# Requires: PowerShell 5+ (already on Windows 10+)\n"+
		"\n"+
		"$ErrorActionPreference = 'Stop'\n"+
		"\n"+
		"$InstallDir = Join-Path $env:ProgramData 'SAI'\n"+
		"$BinarySrc  = Join-Path $PSScriptRoot '%s'\n"+
		"$ConfigSrc  = Join-Path $PSScriptRoot 'config.json'\n"+
		"\n"+
		"Write-Host \"[SAI] Installing to $InstallDir\"\n"+
		"New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null\n"+
		"Copy-Item -Force $BinarySrc (Join-Path $InstallDir 'sai-agent.exe')\n"+
		"Copy-Item -Force $ConfigSrc (Join-Path $InstallDir 'config.json')\n"+
		"\n"+
		"$svcName = 'sai-agent'\n"+
		"$existing = Get-Service -Name $svcName -ErrorAction SilentlyContinue\n"+
		"if ($existing) {\n"+
		"    Write-Host \"[SAI] Stopping existing service\"\n"+
		"    Stop-Service -Name $svcName -Force -ErrorAction SilentlyContinue\n"+
		"    sc.exe delete $svcName | Out-Null\n"+
		"    Start-Sleep -Seconds 2\n"+
		"}\n"+
		"\n"+
		"Write-Host \"[SAI] Creating service\"\n"+
		"New-Item -ItemType Directory -Force -Path 'C:\\Logs\\SAI' | Out-Null\n"+
		"# El binario acepta --log-file y rota en append. Lo invocamos directo\n"+
		"# (sin cmd.exe wrapper) para que el SCM pueda monitorear su lifetime.\n"+
		"$binPath = '\"' + $InstallDir + '\\sai-agent.exe\" --config \"' + $InstallDir + '\\config.json\" --log-file \"C:\\Logs\\SAI\\agent.log\"'\n"+
		"\n"+
		"# Requires admin\n"+
		"sc.exe create $svcName binPath= $binPath start= delayed-auto depend= rpcss | Out-Null\n"+
		"sc.exe description $svcName \"SAI Agent - remote management\" | Out-Null\n"+
		"sc.exe failure $svcName reset= 60 actions= restart/5000/restart/10000/restart/60000 | Out-Null\n"+
		"\n"+
		"Write-Host \"[SAI] Starting service\"\n"+
		"Start-Service -Name $svcName\n"+
		"\n"+
		"Write-Host \"[SAI] Done. Service '$svcName' is running.\"\n"+
		"Write-Host \"[SAI] Logs: C:\\Logs\\SAI\\agent.log\"\n"+
		"Write-Host \"[SAI] To uninstall: Stop-Service sai-agent; sc.exe delete sai-agent; Remove-Item '$InstallDir' -Recurse -Force; Remove-Item 'C:\\Logs\\SAI' -Recurse -Force\"\n",
		serverURL, binaryName)
}

func renderInstallUnix(binaryName, serverURL, osName string) string {
	var unitName, servicePath string
	switch osName {
	case "linux":
		unitName = "sai-agent.service"
		servicePath = "/etc/systemd/system/" + unitName
	default:
		unitName = "com.sai.agent.plist"
		servicePath = "/Library/LaunchDaemons/" + unitName
	}
	return fmt.Sprintf("#!/usr/bin/env bash\n"+
		"# SAI Agent installer for %s\n"+
		"# Server: %s\n"+
		"set -euo pipefail\n"+
		"\n"+
		"if [[ $EUID -ne 0 ]]; then\n"+
		"    echo \"[SAI] Please run as root (sudo ./install.sh)\" >&2\n"+
		"    exit 1\n"+
		"fi\n"+
		"\n"+
		"INSTALL_DIR=/etc/sai\n"+
		"LOG_DIR=/var/log/sai\n"+
		"mkdir -p \"$INSTALL_DIR\" \"$LOG_DIR\"\n"+
		"cp -f \"%s\" \"$INSTALL_DIR/sai-agent\"\n"+
		"cp -f config.json \"$INSTALL_DIR/config.json\"\n"+
		"chmod 0755 \"$INSTALL_DIR/sai-agent\"\n"+
		"chmod 0600 \"$INSTALL_DIR/config.json\"\n"+
		"\n"+
		"case \"%s\" in\n"+
		"  linux)\n"+
		"    cat > %s <<'__SAI_UNIT_EOF__'\n"+
		"[Unit]\n"+
		"Description=SAI Agent - remote management\n"+
		"After=network-online.target\n"+
		"Wants=network-online.target\n"+
		"\n"+
		"[Service]\n"+
		"Type=simple\n"+
		"ExecStart=$INSTALL_DIR/sai-agent --config $INSTALL_DIR/config.json\n"+
		"Restart=always\n"+
		"RestartSec=5\n"+
		"User=root\n"+
		"LimitNOFILE=65536\n"+
		"\n"+
		"[Install]\n"+
		"WantedBy=multi-user.target\n"+
		"__SAI_UNIT_EOF__\n"+
		"    systemctl daemon-reload\n"+
		"    systemctl enable sai-agent\n"+
		"    systemctl start sai-agent\n"+
		"    echo \"[SAI] systemd unit installed and started\"\n"+
		"    ;;\n"+
		"  darwin)\n"+
		"    cat > %s <<'__SAI_PLIST_EOF__'\n"+
		"<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n"+
		"<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n"+
		"<plist version=\"1.0\"><dict>\n"+
		"  <key>Label</key><string>com.sai.agent</string>\n"+
		"  <key>ProgramArguments</key>\n"+
		"  <array>\n"+
		"    <string>$INSTALL_DIR/sai-agent</string>\n"+
		"    <string>--config</string>\n"+
		"    <string>$INSTALL_DIR/config.json</string>\n"+
		"  </array>\n"+
		"  <key>RunAtLoad</key><true/>\n"+
		"  <key>KeepAlive</key><true/>\n"+
		"  <key>StandardOutPath</key><string>$LOG_DIR/sai-agent.out.log</string>\n"+
		"  <key>StandardErrorPath</key><string>$LOG_DIR/sai-agent.err.log</string>\n"+
		"</dict></plist>\n"+
		"__SAI_PLIST_EOF__\n"+
		"    launchctl load -w %s\n"+
		"    echo \"[SAI] launchd plist installed and loaded\"\n"+
		"    ;;\n"+
		"esac\n"+
		"\n"+
		"echo \"[SAI] Done. Logs: $LOG_DIR/\"\n",
		osName, serverURL, binaryName, osName, servicePath, servicePath, servicePath)
}

func renderReadme(binaryName string, windows bool) string {
	if windows {
		return fmt.Sprintf("SAI Agent Bundle\n"+
			"================\n"+
			"\n"+
			"Contiene:\n"+
			"  - %s      Binario del agente\n"+
			"  - config.json     Configuracion (server URL + token de enrolamiento)\n"+
			"  - install.ps1     Script de instalacion (ejecutar como Administrador)\n"+
			"\n"+
			"Instalacion:\n"+
			"  1. Descomprime este ZIP\n"+
			"  2. Click derecho en install.ps1 -> Run with PowerShell (Admin)\n"+
			"     (o: powershell -ExecutionPolicy Bypass -File install.ps1)\n"+
			"\n"+
			"El servicio 'sai-agent' quedara registrado e iniciara automaticamente.\n",
			binaryName)
	}
	return fmt.Sprintf("SAI Agent Bundle\n"+
		"================\n"+
		"\n"+
		"Contiene:\n"+
		"  - %s      Binario del agente\n"+
		"  - config.json     Configuracion (server URL + token de enrolamiento)\n"+
		"  - install.sh      Script de instalacion (ejecutar como root)\n"+
		"\n"+
		"Instalacion:\n"+
		"  1. Descomprime este ZIP\n"+
		"  2. sudo ./install.sh\n"+
		"\n"+
		"El servicio sai-agent quedara registrado e iniciara automaticamente.\n",
		binaryName)
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

func normalizeOS(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "windows", "win":
		return "windows"
	case "linux":
		return "linux"
	case "darwin", "mac", "macos":
		return "darwin"
	}
	return s
}

func normalizeArch(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "amd64", "x86_64", "x64":
		return "amd64"
	case "arm64", "aarch64":
		return "arm64"
	}
	return s
}

func validOS(s string) bool { return s == "windows" || s == "linux" || s == "darwin" }
func validArch(s string) bool { return s == "amd64" || s == "arm64" }