package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEdgeHostDefault(t *testing.T) {
	dir := t.TempDir() // no edge_runtime.json
	t.Setenv("HOC_ROOT", dir)
	t.Setenv("HOC_EDGE_HOST", "")
	t.Setenv("HOC_EDGE_MODE", "")
	t.Setenv("HOC_GS_GATEWAY", "")

	host, mode, gw := resolveEdge()
	if host != DefaultEdgeHost || mode != "hostname" || gw != "" {
		t.Fatalf("default resolveEdge=(%q,%q,%q)", host, mode, gw)
	}
}

func TestEdgeHostEnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOC_ROOT", dir)
	t.Setenv("HOC_EDGE_HOST", "192.168.68.111")
	t.Setenv("HOC_EDGE_MODE", "")
	t.Setenv("HOC_GS_GATEWAY", "")

	host, mode, _ := resolveEdge()
	if host != "192.168.68.111" {
		t.Fatalf("env EdgeHost=%q", host)
	}
	// An explicit non-default host must imply a LAN edge even without mode.
	if mode != "lan" {
		t.Fatalf("inferred mode=%q, want lan", mode)
	}
}

func writeRuntime(t *testing.T, dir string, payload map[string]string) {
	t.Helper()
	b, _ := json.Marshal(payload)
	if err := os.WriteFile(filepath.Join(dir, "edge_runtime.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEdgeHostFileFallback(t *testing.T) {
	dir := t.TempDir()
	writeRuntime(t, dir, map[string]string{"edge_host": "10.0.0.5"})
	t.Setenv("HOC_ROOT", dir)
	t.Setenv("HOC_EDGE_HOST", "")
	t.Setenv("HOC_EDGE_MODE", "")
	t.Setenv("HOC_GS_GATEWAY", "")

	host, _, _ := resolveEdge()
	if host != "10.0.0.5" {
		t.Fatalf("file EdgeHost=%q", host)
	}
}

// D1 regression: the APK pipeline writes all three fields; mode and gs_gateway
// used to be ignored, leaving the phone with a Nox gs= in e02d.
func TestEdgeRuntimeFileParsesModeAndGateway(t *testing.T) {
	oldGw, oldMode, oldHost := GSGateway, EdgeMode, EdgeHost
	defer func() { GSGateway, EdgeMode, EdgeHost = oldGw, oldMode, oldHost }()

	dir := t.TempDir()
	writeRuntime(t, dir, map[string]string{
		"edge_host": "192.168.68.111", "gs_gateway": "192.168.68.111", "mode": "lan",
	})
	t.Setenv("HOC_ROOT", dir)
	t.Setenv("HOC_EDGE_HOST", "")
	t.Setenv("HOC_EDGE_MODE", "")
	t.Setenv("HOC_GS_GATEWAY", "")

	EdgeHost, EdgeMode, GSGateway = resolveEdge()
	if EdgeHost != "192.168.68.111" || EdgeMode != "lan" || GSGateway != "192.168.68.111" {
		t.Fatalf("file-only resolveEdge=(%q,%q,%q)", EdgeHost, EdgeMode, GSGateway)
	}
	if got := EffectiveGSGateway(); got != "192.168.68.111" {
		t.Fatalf("file-only EffectiveGSGateway=%q, want LAN IP", got)
	}
	if !IsLANEdge() {
		t.Fatal("IsLANEdge=false for file-only lan config")
	}
}

// Env must still beat the file (escape hatch).
func TestEdgeRuntimeEnvBeatsFile(t *testing.T) {
	dir := t.TempDir()
	writeRuntime(t, dir, map[string]string{
		"edge_host": "192.168.68.111", "gs_gateway": "192.168.68.111", "mode": "lan",
	})
	t.Setenv("HOC_ROOT", dir)
	t.Setenv("HOC_EDGE_HOST", "10.1.2.3")
	t.Setenv("HOC_EDGE_MODE", "hostname")
	t.Setenv("HOC_GS_GATEWAY", "10.9.9.9")

	host, mode, gw := resolveEdge()
	if host != "10.1.2.3" || mode != "hostname" || gw != "10.9.9.9" {
		t.Fatalf("env-over-file resolveEdge=(%q,%q,%q)", host, mode, gw)
	}
}

// Nox anchor: a malformed or absent file must never move the default path.
func TestEdgeRuntimeGarbageKeepsNoxDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "edge_runtime.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOC_ROOT", dir)
	t.Setenv("HOC_EDGE_HOST", "")
	t.Setenv("HOC_EDGE_MODE", "")
	t.Setenv("HOC_GS_GATEWAY", "")

	host, mode, gw := resolveEdge()
	if host != DefaultEdgeHost || mode != "hostname" || gw != "" {
		t.Fatalf("garbage resolveEdge=(%q,%q,%q)", host, mode, gw)
	}
}

// D2 helper: Nox-pool gateways are stale only while serving a LAN edge.
func TestStaleNoxGateway(t *testing.T) {
	oldGw, oldMode, oldHost := GSGateway, EdgeMode, EdgeHost
	defer func() { GSGateway, EdgeMode, EdgeHost = oldGw, oldMode, oldHost }()

	EdgeMode, EdgeHost = "lan", "192.168.68.111"
	for _, gw := range []string{"172.16.42.2", "172.16.57.2", DefaultGSIP} {
		if !StaleNoxGateway(gw) {
			t.Fatalf("StaleNoxGateway(%q)=false on lan edge", gw)
		}
	}
	if StaleNoxGateway("192.168.68.111") {
		t.Fatal("LAN gateway marked stale")
	}
	if StaleNoxGateway("") {
		t.Fatal("empty gateway marked stale")
	}

	// Nox mode: nothing is stale — the pool is correct there.
	EdgeMode, EdgeHost = "hostname", DefaultEdgeHost
	for _, gw := range []string{"172.16.42.2", "172.16.57.2", DefaultGSIP} {
		if StaleNoxGateway(gw) {
			t.Fatalf("StaleNoxGateway(%q)=true on Nox edge", gw)
		}
	}
}

func TestEffectiveGSGatewayDefault(t *testing.T) {
	oldGw, oldMode, oldHost := GSGateway, EdgeMode, EdgeHost
	defer func() { GSGateway, EdgeMode, EdgeHost = oldGw, oldMode, oldHost }()

	GSGateway = ""
	EdgeMode = "hostname"
	EdgeHost = "game-portal.gameloft.com"
	if got := EffectiveGSGateway(); got != DefaultGSIP {
		t.Fatalf("default EffectiveGSGateway=%q", got)
	}
}

func TestEffectiveGSGatewayLAN(t *testing.T) {
	oldGw, oldMode, oldHost := GSGateway, EdgeMode, EdgeHost
	defer func() { GSGateway, EdgeMode, EdgeHost = oldGw, oldMode, oldHost }()

	GSGateway = ""
	EdgeMode = "lan"
	EdgeHost = "192.168.68.111"
	if got := EffectiveGSGateway(); got != "192.168.68.111" {
		t.Fatalf("lan EffectiveGSGateway=%q", got)
	}
}

func TestEffectiveGSGatewayExplicit(t *testing.T) {
	oldGw, oldMode, oldHost := GSGateway, EdgeMode, EdgeHost
	defer func() { GSGateway, EdgeMode, EdgeHost = oldGw, oldMode, oldHost }()

	GSGateway = "10.0.0.99"
	EdgeMode = "lan"
	EdgeHost = "192.168.68.111"
	if got := EffectiveGSGateway(); got != "10.0.0.99" {
		t.Fatalf("explicit EffectiveGSGateway=%q", got)
	}
}

// --- WAN / public edge contract (2026-08-16, AGENTS5 problem H) -----------
//
// Home line is behind CGNAT (traceroute hop 100.100.0.1 = RFC6598) so
// port-forward is impossible; the deploy target is a VPS with a real public
// address. These lock the mode=public semantics before that box exists.

// mode=public must select the operator address for the GS relay. Regression:
// before 2026-08-16 IsLANEdge() only matched "lan", so a public deploy
// advertised the Nox pool IP (172.16.42.2) in e02d — unreachable on WAN.
func TestPublicModeUsesOperatorGateway(t *testing.T) {
	prevHost, prevMode, prevGW := EdgeHost, EdgeMode, GSGateway
	defer func() { EdgeHost, EdgeMode, GSGateway = prevHost, prevMode, prevGW }()

	EdgeHost, EdgeMode, GSGateway = "203.0.113.9", "public", ""
	if !IsDirectEdge() {
		t.Fatal("mode=public must be a direct edge")
	}
	if got := EffectiveGSGateway(); got != "203.0.113.9" {
		t.Fatalf("public GS gateway = %q, want the public host (Nox pool leak?)", got)
	}
}

// The Nox DNAT default must stay untouched by the public-mode work.
func TestHostnameModeKeepsNoxDefaults(t *testing.T) {
	prevHost, prevMode, prevGW := EdgeHost, EdgeMode, GSGateway
	defer func() { EdgeHost, EdgeMode, GSGateway = prevHost, prevMode, prevGW }()

	EdgeHost, EdgeMode, GSGateway = DefaultEdgeHost, "hostname", ""
	if IsDirectEdge() {
		t.Fatal("default hostname path must not be a direct edge")
	}
	if got := EffectiveGSGateway(); got != DefaultGSIP {
		t.Fatalf("Nox gateway = %q, want %q", got, DefaultGSIP)
	}
	if got := ClientHost(); got != DefaultEdgeHost {
		t.Fatalf("ClientHost = %q, want %q", got, DefaultEdgeHost)
	}
}

// ClientHost may serve a custom DNS name on WAN, but must NEVER return a bare
// IP: the cert has DNS SANs only, and an IP there caused the 2026-08-16
// "checking update..." /locate loop.
func TestClientHostOverrideAcceptsNameRejectsIP(t *testing.T) {
	prev := ClientHostOverride
	defer func() { ClientHostOverride = prev }()

	ClientHostOverride = "hoc.example.com"
	if got := ClientHost(); got != "hoc.example.com" {
		t.Fatalf("ClientHost = %q, want the override hostname", got)
	}

	for _, bad := range []string{"203.0.113.9", "192.168.68.111", "::1", "[2001:db8::1]"} {
		ClientHostOverride = bad
		if got := ClientHost(); got != DefaultEdgeHost {
			t.Fatalf("ClientHost(%q) = %q, want fallback %q — bare IP breaks TLS SNI/SAN",
				bad, got, DefaultEdgeHost)
		}
	}
}
