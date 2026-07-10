// Command smoketest verifica end-to-end que el server funciona:
//
//  1. /api/v1/health          (público)
//  2. /api/v1/version         (público)
//  3. /api/v1/auth/login      (autenticación)
//  4. /api/v1/auth/me         (con cookie)
//  5. /api/v1/dashboard/summary
//  6. POST /api/v1/tokens     (crea enrollment token con CSRF)
//  7. /api/v1/audit/events
//  8. /api/v1/inventory endpoints (Fase 2) — verifican shape de respuesta,
//     404 si no hay inventario y 400 para id inválido. NO requiere un
//     agente real conectado; el flujo completo se valida en
//     scripts/verify-docker.sh + un agente local.
//  9. /api/v1/auth/logout
//
// Uso:
//
//	SAI_URL=http://127.0.0.1:8080 \
//	SAI_ADMIN_EMAIL=admin@sai.local \
//	SAI_ADMIN_PASSWORD=Test#2026 \
//	go run ./cmd/smoketest
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"
)

var pass, fail int

type apiResp struct {
	status int
	body   []byte
	ct     string
}

func main() {
	base := os.Getenv("SAI_URL")
	if base == "" {
		base = "http://127.0.0.1:8080"
	}
	email := os.Getenv("SAI_ADMIN_EMAIL")
	password := os.Getenv("SAI_ADMIN_PASSWORD")
	if email == "" {
		email = "admin@sai.local"
	}
	if password == "" {
		password = "Test#2026"
	}

	fmt.Printf("=== SAI smoke test ===\n")
	fmt.Printf("URL:       %s\n", base)
	fmt.Printf("Email:     %s\n", email)
	fmt.Printf("\n")

	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar:     jar,
		Timeout: 5 * time.Second,
	}

	check := func(name string, code, expect int, body []byte) {
		if code == expect {
			fmt.Printf("  [PASS] %s -> %d\n", name, code)
			pass++
		} else {
			fmt.Printf("  [FAIL] %s -> got %d, expected %d\n", name, code, expect)
			fmt.Printf("         body: %s\n", truncate(string(body), 200))
			fail++
		}
	}

	get := func(path string) apiResp { return do(client, "GET", base+path, nil) }
	post := func(path string, body any) apiResp {
		var r io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			r = bytes.NewReader(b)
		}
		return do(client, "POST", base+path, r)
	}

	// 1. Health
	fmt.Println("[1] /api/v1/health (público, debe ser 200)")
	r := get("/api/v1/health")
	check("health", r.status, 200, r.body)

	// 2. Version
	fmt.Println("[2] /api/v1/version (público)")
	r = get("/api/v1/version")
	check("version", r.status, 200, r.body)

	// 3. Auth/me sin sesión (debe ser 401)
	fmt.Println("[3] /api/v1/auth/me sin sesión (debe ser 401)")
	r = get("/api/v1/auth/me")
	check("auth/me sin sesión", r.status, 401, r.body)

	// 4. Login
	fmt.Println("[4] POST /api/v1/auth/login")
	r = post("/api/v1/auth/login", map[string]string{
		"email":    email,
		"password": password,
	})
	check("login", r.status, 200, r.body)
	if r.status != 200 {
		fmt.Println("\nCannot continue without login; aborting.")
		os.Exit(1)
	}
	var loginResp struct {
		User   map[string]any `json:"user"`
		CSRF   string         `json:"csrf"`
		Expiry string         `json:"expires_at"`
	}
	_ = json.Unmarshal(r.body, &loginResp)
	csrf := loginResp.CSRF
	fmt.Printf("         user.email = %v\n", loginResp.User["email"])
	fmt.Printf("         csrf       = %s...\n", csrf[:min(16, len(csrf))])

	// 5. /auth/me autenticado
	fmt.Println("[5] /api/v1/auth/me con cookie (debe ser 200)")
	r = get("/api/v1/auth/me")
	check("auth/me autenticado", r.status, 200, r.body)

	// 6. Dashboard
	fmt.Println("[6] /api/v1/dashboard/summary")
	r = get("/api/v1/dashboard/summary")
	check("dashboard/summary", r.status, 200, r.body)
	if r.status == 200 {
		var sum struct {
			KPIs struct {
				Online  int `json:"agents_online"`
				Offline int `json:"agents_offline"`
				Tokens  int `json:"active_tokens"`
				Jobs    int `json:"running_jobs"`
			} `json:"kpis"`
		}
		_ = json.Unmarshal(r.body, &sum)
		fmt.Printf("         KPIs: online=%d offline=%d tokens=%d jobs=%d\n",
			sum.KPIs.Online, sum.KPIs.Offline, sum.KPIs.Tokens, sum.KPIs.Jobs)
	}

	// 7. Crear token (POST con CSRF)
	fmt.Println("[7] POST /api/v1/tokens (con CSRF)")
	r = withCSRF(client, "POST", base+"/api/v1/tokens", csrf, map[string]any{
		"label":     "smoke-test-bundle",
		"max_uses":  8, // 6 downloads + 1 para el 400 + margen
		"ttl_hours": 24,
	})
	check("crear token", r.status, 201, r.body)
	var tokResp struct {
		Plain        string `json:"plain"`
		DownloadURLs []struct {
			OS   string `json:"os"`
			Arch string `json:"arch"`
			URL  string `json:"url"`
		} `json:"download_urls"`
	}
	_ = json.Unmarshal(r.body, &tokResp)
	enrollToken := tokResp.Plain
	urls := map[string]string{} // key "os/arch" -> url
	for _, p := range tokResp.DownloadURLs {
		urls[p.OS+"/"+p.Arch] = p.URL
	}
	fmt.Printf("         enroll token (24 chars): %s...\n", enrollToken[:min(24, len(enrollToken))])
	fmt.Printf("         download_urls count: %d (esperado: 6)\n", len(tokResp.DownloadURLs))
	if len(tokResp.DownloadURLs) == 6 {
		fmt.Println("         [PASS] download_urls tiene 6 plataformas")
		pass++
	} else {
		fmt.Printf("         [FAIL] download_urls tiene %d, esperado 6\n", len(tokResp.DownloadURLs))
		fail++
	}

	// 8. Listar tokens
	fmt.Println("[8] GET /api/v1/tokens")
	r = get("/api/v1/tokens")
	check("listar tokens", r.status, 200, r.body)

	// 9. Listar agentes
	fmt.Println("[9] GET /api/v1/agents")
	r = get("/api/v1/agents")
	check("listar agentes", r.status, 200, r.body)

	// 10. Listar grupos
	fmt.Println("[10] GET /api/v1/groups")
	r = get("/api/v1/groups")
	check("listar grupos", r.status, 200, r.body)

	// 11. Listar templates
	fmt.Println("[11] GET /api/v1/templates")
	r = get("/api/v1/templates")
	check("listar templates", r.status, 200, r.body)
	if r.status == 200 {
		var t struct {
			Items []struct {
				Name     string `json:"name"`
				IsBuiltin bool  `json:"is_builtin"`
			} `json:"items"`
		}
		_ = json.Unmarshal(r.body, &t)
		fmt.Printf("         %d templates cargados:\n", len(t.Items))
		for i, item := range t.Items {
			if i >= 6 {
				fmt.Printf("         ... (+%d más)\n", len(t.Items)-6)
				break
			}
			builtin := ""
			if item.IsBuiltin {
				builtin = " [builtin]"
			}
			fmt.Printf("           - %s%s\n", item.Name, builtin)
		}
	}

	// 12. Audit
	fmt.Println("[12] GET /api/v1/audit/events")
	r = get("/api/v1/audit/events?per_page=5")
	check("audit/events", r.status, 200, r.body)

	// 13-15. Fase 2: endpoints de inventario. Como no hay agentes enrolados
	// durante el smoketest, los GET deben devolver 404. El POST refresh
	// devuelve 400 con UUID inválido. Verificamos el shape de respuesta del
	// panel y la consistencia del contrato HTTP.Estos tests requieren
	// sesión activa: van ANTES del logout (test #16).
	fmt.Println("[13] GET /api/v1/agents/{no-agent}/inventory (404 esperado)")
	r = get("/api/v1/agents/00000000-0000-0000-0000-000000000000/inventory")
	check("inventory latest con agente inexistente", r.status, 404, r.body)

	fmt.Println("[14] GET /api/v1/agents/{no-agent}/inventory/history (200 con items=[])")
	r = get("/api/v1/agents/00000000-0000-0000-0000-000000000000/inventory/history?limit=10")
	check("inventory history con agente inexistente", r.status, 200, r.body)
	if r.status == 200 {
		var h struct {
			Items []map[string]any `json:"items"`
		}
		// Acepta items=[] o items=null cuando no hay agente.
		_ = json.Unmarshal(r.body, &h)
		if h.Items != nil && len(h.Items) != 0 {
			fmt.Printf("         aviso: esperado items=[]/null cuando el agente no existe, got %d items\n", len(h.Items))
		}
	}

	fmt.Println("[15] POST /api/v1/agents/{no}/inventory/refresh (400 id inválido)")
	r = withCSRF(client, "POST", base+"/api/v1/agents/no-valid-uuid/inventory/refresh", csrf, nil)
	check("inventory refresh con id inválido", r.status, 400, r.body)

	// 16. Logout (POST con CSRF)
	fmt.Println("[16] POST /api/v1/auth/logout (con CSRF)")
	r = withCSRF(client, "POST", base+"/api/v1/auth/logout", csrf, nil)
	check("logout", r.status, 200, r.body)

	// 17. /auth/me post-logout (debe ser 401)
	fmt.Println("[17] /api/v1/auth/me después de logout (debe ser 401)")
	r = get("/api/v1/auth/me")
	check("auth/me post-logout", r.status, 401, r.body)

	// 18-20. Descarga de bundle ZIP para los 3 OS principales
	fmt.Println()
	fmt.Println("[18-20] Descarga de bundles (binarios del agente)")
	for _, tgt := range []struct{ os, arch string }{
		{"windows", "amd64"},
		{"linux", "amd64"},
		{"darwin", "amd64"},
	} {
		r = downloadBundle(base, enrollToken, tgt.os, tgt.arch)
		ct := r.ct
		expectedCT := "application/zip"
		ok := r.status == 200 && len(r.body) > 1024 && (ct == expectedCT || strings.Contains(ct, "zip"))
		if ok {
			fmt.Printf("  [PASS] bundle %s/%s -> %d bytes, ct=%s\n", tgt.os, tgt.arch, len(r.body), ct)
			pass++
		} else {
			fmt.Printf("  [FAIL] bundle %s/%s -> status=%d ct=%s size=%d. body: %s\n",
				tgt.os, tgt.arch, r.status, ct, len(r.body), truncate(string(r.body), 200))
			fail++
		}
	}

	// 21. Token sin canjear no debe servir bundle (otro token random)
	fmt.Println("[21] /api/v1/agents/download con token inválido (debe ser 403)")
	r = downloadBundleRaw(base+"/api/v1/agents/download?token=invalid-token-xxx&os=windows&arch=amd64")
	check("download token inválido", r.status, 403, r.body)

	// 22. Descarga sin ?os= o sin ?arch= debe dar 400 (no fallback al server's OS)
	fmt.Println("[22] Download sin ?os= o sin ?arch= debe ser 400")
	r = downloadBundleRaw(base + "/api/v1/agents/download?token=" + enrollToken)
	check("download sin os/arch", r.status, 400, r.body)
	r = downloadBundleRaw(base + "/api/v1/agents/download?token=" + enrollToken + "&os=windows")
	check("download sin arch", r.status, 400, r.body)

	// 23-24. Descarga de los 3 targets restantes (arm64) — valida que el array
	// de download_urls funciona para todas las plataformas, no solo amd64.
	fmt.Println("[23-24] Descarga de bundles arm64 (los 3 OS)")
	for _, tgt := range []struct{ os, arch string }{
		{"windows", "arm64"},
		{"linux", "arm64"},
		{"darwin", "arm64"},
	} {
		r = downloadBundle(base, enrollToken, tgt.os, tgt.arch)
		ok := r.status == 200 && len(r.body) > 1024 && strings.Contains(r.ct, "zip")
		if ok {
			fmt.Printf("  [PASS] bundle %s/%s -> %d bytes\n", tgt.os, tgt.arch, len(r.body))
			pass++
		} else {
			fmt.Printf("  [FAIL] bundle %s/%s -> status=%d size=%d ct=%s\n",
				tgt.os, tgt.arch, r.status, len(r.body), r.ct)
			fail++
		}
	}

	// 25-27. Fase 3 / DT-5: dispatch end-to-end. Requiere al menos un
	// agente enrollado y conectado. Si no hay agentes (smoketest en CI),
	// skipeamos con SKIP (no falla). En local con un agente corriendo,
	// verifica que el ciclo pending -> dispatched -> completed funciona
	// via la API y que el GET del item muestra stdout + exit_code.
	fmt.Println("[25-27] Fase 3: dispatch end-to-end (skip si no hay agentes)")
	p, skipped, fc := runDispatchE2E(base, "", client)
	pass += p
	fail += fc
	if skipped {
		fmt.Println("  [SKIP] no hay agentes conectados; los 3 tests se omiten")
	}

	// Resumen
	fmt.Println()
	fmt.Println("===============================================")
	fmt.Printf("Resultado: %d/%d tests passed\n", pass, pass+fail)
	if fail > 0 {
		fmt.Println("FAILED")
		os.Exit(1)
	}
	fmt.Println("OK")
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

// downloadBundle hace GET al endpoint de descarga del agente y devuelve
// el ZIP como bytes (junto con status y content-type).
func downloadBundle(base, token, osName, arch string) apiResp {
	return downloadBundleRaw(base + "/api/v1/agents/download?token=" + token + "&os=" + osName + "&arch=" + arch)
}

func downloadBundleRaw(urlStr string) apiResp {
	req, _ := http.NewRequest("GET", urlStr, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return apiResp{status: -1, body: []byte(err.Error())}
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return apiResp{status: resp.StatusCode, body: b, ct: resp.Header.Get("Content-Type")}
}

func do(client *http.Client, method, url string, body io.Reader) apiResp {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return apiResp{status: -1, body: []byte(err.Error())}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return apiResp{status: -1, body: []byte(err.Error())}
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return apiResp{status: resp.StatusCode, body: b, ct: resp.Header.Get("Content-Type")}
}

func withCSRF(client *http.Client, method, urlStr, csrf string, body any) apiResp {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, urlStr, r)
	if err != nil {
		return apiResp{status: -1, body: []byte(err.Error())}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-CSRF-Token", csrf)
	resp, err := client.Do(req)
	if err != nil {
		return apiResp{status: -1, body: []byte(err.Error())}
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return apiResp{status: resp.StatusCode, body: b}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// runDispatchE2E ejecuta el ciclo dispatch end-to-end de Fase 3 / DT-5.
// Devuelve (passed, skipped, failCount).
//   - passed: cantidad de sub-tests que pasaron (0..3).
//   - skipped: true si no hay agentes conectados (CI sin agente).
//   - failCount: cantidad de sub-tests que fallaron.
func runDispatchE2E(base, csrfIn string, client *http.Client) (passed int, skipped bool, failCount int) {
	// Re-login porque el test 13 hizo logout y borra la cookie del jar.
	email := os.Getenv("SAI_ADMIN_EMAIL")
	password := os.Getenv("SAI_ADMIN_PASSWORD")
	if email == "" {
		email = "admin@sai.local"
	}
	if password == "" {
		password = "Test#2026"
	}
	csrf := csrfIn
	if csrf == "" {
		csrf = "skipped-if-empty" // placeholder; will be replaced by login response
	}
	{
		body, _ := json.Marshal(map[string]string{"email": email, "password": password})
		req, _ := http.NewRequest("POST", base+"/api/v1/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			bb, _ := io.ReadAll(resp.Body)
			var lr struct {
				CSRF string `json:"csrf"`
			}
			_ = json.Unmarshal(bb, &lr)
			if lr.CSRF != "" {
				csrf = lr.CSRF
			}
		}
	}

	// 25. Listar agentes y tomar el primero.
	r := do(client, "GET", base+"/api/v1/agents?per_page=1", nil)
	if r.status != 200 {
		fmt.Printf("  [FAIL] [25] GET /agents -> status=%d body=%s\n", r.status, truncate(string(r.body), 200))
		return 0, false, 1
	}
	var listResp struct {
		Items []struct {
			ID       string `json:"id"`
			Hostname string `json:"hostname"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(r.body, &listResp); err != nil {
		fmt.Printf("  [FAIL] [25] GET /agents parse: %v\n", err)
		return 0, false, 1
	}
	if listResp.Total == 0 || len(listResp.Items) == 0 {
		return 0, true, 0 // SKIP
	}
	agentID := listResp.Items[0].ID
	fmt.Printf("  [PASS] [25] GET /agents -> %d agentes, target=%s\n", listResp.Total, agentID)

	// 26. Crear job inline target=agent con echo hello (cross-platform).
	cmd := "cmd"
	args := []string{"/c", "echo", "hello from smoketest"}
	if runtime.GOOS != "windows" {
		cmd = "sh"
		args = []string{"-c", "echo 'hello from smoketest'"}
	}
	jobBody := map[string]any{
		"name":           "smoketest-dispatch",
		"source":         "inline",
		"inline_command": cmd,
		"inline_args":    args,
		"inline_timeout": 10,
		"target_type":    "agent",
		"target_id":      agentID,
	}
	r = withCSRF(client, "POST", base+"/api/v1/jobs", csrf, jobBody)
	if r.status != 201 {
		fmt.Printf("  [FAIL] [26] POST /jobs -> status=%d body=%s\n", r.status, truncate(string(r.body), 200))
		return 1, false, 1
	}
	var jobResp struct {
		ID         string `json:"id"`
		TotalItems int    `json:"total_items"`
	}
	if err := json.Unmarshal(r.body, &jobResp); err != nil {
		fmt.Printf("  [FAIL] [26] parse: %v\n", err)
		return 1, false, 1
	}
	if jobResp.TotalItems < 1 {
		fmt.Printf("  [FAIL] [26] job sin items: %+v\n", jobResp)
		return 1, false, 1
	}
	fmt.Printf("  [PASS] [26] POST /jobs -> job_id=%s items=%d\n", jobResp.ID, jobResp.TotalItems)

	// 27. Esperar a que el item termine y verificar stdout.
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		r = do(client, "GET", base+"/api/v1/jobs/"+jobResp.ID+"/items?per_page=10", nil)
		if r.status != 200 {
			continue
		}
		var itemsResp struct {
			Items []struct {
				ID       string `json:"id"`
				Status   string `json:"status"`
				ExitCode *int   `json:"exit_code"`
				Stdout   string `json:"stdout"`
				Stderr   string `json:"stderr"`
				ErrorMsg string `json:"error_msg"`
			} `json:"items"`
		}
		_ = json.Unmarshal(r.body, &itemsResp)
		for _, it := range itemsResp.Items {
			if it.Status == "completed" || it.Status == "failed" || it.Status == "timeout" || it.Status == "offline" {
				if it.Status != "completed" {
					fmt.Printf("  [FAIL] [27] item termino con status=%s exit=%v err=%q\n",
						it.Status, derefIntPtr(it.ExitCode), it.ErrorMsg)
					return 2, false, 1
				}
				if it.ExitCode == nil || *it.ExitCode != 0 {
					fmt.Printf("  [FAIL] [27] exit_code != 0 (got %v)\n", it.ExitCode)
					return 2, false, 1
				}
				if !strings.Contains(it.Stdout, "hello") {
					fmt.Printf("  [FAIL] [27] stdout no contiene 'hello' (got %q)\n", truncate(it.Stdout, 80))
					return 2, false, 1
				}
				fmt.Printf("  [PASS] [27] item completed: exit=0 stdout=%q\n", truncate(it.Stdout, 60))
				return 3, false, 0
			}
		}
	}
	fmt.Printf("  [FAIL] [27] item no termino en 8s (ultimo status no era terminal)\n")
	return 2, false, 1
}

func derefIntPtr(p *int) int {
	if p == nil {
		return -1
	}
	return *p
}

// _ silencia import no usado si en algún momento agregamos
var _ = strings.TrimSpace
var _ = url.Parse