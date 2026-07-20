package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseCurrentNATTemplate(t *testing.T) {
	result := ParseFile(repositoryFile(t, "vagrant_setup_scripts", "vagrant-config", "nat_network-config.rb"))
	if !result.Safe || !result.Valid || !result.Complete || result.Status != "SAFE_COMPLETE" {
		t.Fatalf("result = %#v", result)
	}
	if result.Config.NodeCount != 5 || result.Config.Network.Mode != "nat" || result.Config.Resources.TotalCPU != 26 || result.Config.Resources.TotalMemoryMiB != 32768 {
		t.Fatalf("config = %#v", result.Config)
	}
	if len(result.Config.ExpectedNodes()) != 5 || result.Config.ExpectedNodes()[1].Role != "upm-service-worker" {
		t.Fatalf("expected nodes = %#v", result.Config.ExpectedNodes())
	}
}

func TestParseCurrentBridgeTemplateIsSafeIncomplete(t *testing.T) {
	result := ParseFile(repositoryFile(t, "vagrant_setup_scripts", "vagrant-config", "bridge_network-config.rb"))
	if !result.Safe || !result.Valid || result.Complete || result.Status != "SAFE_INCOMPLETE" {
		t.Fatalf("result = %#v", result)
	}
}

func TestParseRejectsActiveRuby(t *testing.T) {
	path := copyNATTemplate(t)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("\nsystem(\"touch /tmp/unsafe\")\n"); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	result := ParseFile(path)
	if result.Safe || result.Status != "UNSAFE" || !hasFinding(result.Findings, "UNSAFE_RUBY_SYNTAX") {
		t.Fatalf("result = %#v", result)
	}
}

func TestParseRejectsDuplicateField(t *testing.T) {
	path := copyNATTemplate(t)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("\n$num_instances = 6\n"); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	result := ParseFile(path)
	if result.Safe || !hasFinding(result.Findings, "DUPLICATE_FIELD") {
		t.Fatalf("result = %#v", result)
	}
}

func TestParseDoesNotLeakSensitiveProxyValue(t *testing.T) {
	path := copyNATTemplate(t)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	secret := "super-secret-password"
	contents = append(contents, []byte("\n$http_proxy = \"#{"+secret+"}\"\n")...)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	result := ParseFile(path)
	for _, finding := range result.Findings {
		if strings.Contains(finding.Message, secret) {
			t.Fatalf("finding leaked secret: %#v", finding)
		}
	}
}

func TestValidateRejectsShellSensitiveTimezone(t *testing.T) {
	path := copyNATTemplate(t)
	replaceInFile(t, path, `$time_zone = "Asia/Shanghai"`, `$time_zone = "Asia/Shanghai;touch"`)
	result := ParseFile(path)
	if result.Valid || !hasFinding(result.Findings, "INVALID_TIME_ZONE") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRejectsCiliumLoadBalancerDHCPOverlap(t *testing.T) {
	path := copyNATTemplate(t)
	replacements := map[string]string{
		`$network_plugin = "calico"`:             `$network_plugin = "cilium"`,
		`$cilium_kube_proxy_replacement = false`: `$cilium_kube_proxy_replacement = true`,
		`$cilium_load_balancer_enabled = false`:  `$cilium_load_balancer_enabled = true`,
		`$cilium_load_balancer_start = ""`:       `$cilium_load_balancer_start = "192.168.200.50"`,
		`$cilium_load_balancer_stop = ""`:        `$cilium_load_balancer_stop = "192.168.200.60"`,
	}
	for oldValue, newValue := range replacements {
		replaceInFile(t, path, oldValue, newValue)
	}
	result := ParseFile(path)
	if result.Valid || !hasFinding(result.Findings, "CILIUM_LB_DHCP_OVERLAP") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRejectsUnpinnedKubernetesVersion(t *testing.T) {
	path := copyNATTemplate(t)
	replaceInFile(t, path, `$kube_version = "1.36.1"`, `$kube_version = "9.9.9"`)
	result := ParseFile(path)
	if result.Valid || !hasFinding(result.Findings, "INVALID_KUBERNETES_VERSION") {
		t.Fatalf("result = %#v", result)
	}
}

func repositoryFile(t *testing.T, parts ...string) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(current), "..", "..", ".."))
	return filepath.Join(append([]string{root}, parts...)...)
}

func copyNATTemplate(t *testing.T) string {
	t.Helper()
	contents, err := os.ReadFile(repositoryFile(t, "vagrant_setup_scripts", "vagrant-config", "nat_network-config.rb"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.rb")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func hasFinding(findings []Finding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func replaceInFile(t *testing.T, path, oldValue, newValue string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(contents), oldValue, newValue, 1)
	if updated == string(contents) {
		t.Fatalf("fixture did not contain %q", oldValue)
	}
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
}
