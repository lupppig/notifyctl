package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	// 1. Test loading with non-existent file (should return default)
	cfg, err := Load("non-existent.yaml")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.ServerAddr != DefaultServerAddr {
		t.Errorf("Expected default server addr %s, got %s", DefaultServerAddr, cfg.ServerAddr)
	}

	// 2. Test loading from a real file
	tmpDir, err := os.MkdirTemp("", "notifyctl-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, ".notifyctl.yaml")
	configData := `
server_addr: "localhost:9090"
service_id: "test-service"
api_key: "test-key"
`
	if err := os.WriteFile(configPath, []byte(configData), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err = Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.ServerAddr != "localhost:9090" {
		t.Errorf("Expected server addr localhost:9090, got %s", cfg.ServerAddr)
	}
	if cfg.ServiceID != "test-service" {
		t.Errorf("Expected service id test-service, got %s", cfg.ServiceID)
	}
	if cfg.APIKey != "test-key" {
		t.Errorf("Expected api key test-key, got %s", cfg.APIKey)
	}

	// 3. Test environment overrides
	os.Setenv("NOTIFYCTL_SERVER_ADDR", "env-localhost:8080")
	defer os.Unsetenv("NOTIFYCTL_SERVER_ADDR")

	cfg, err = Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.ServerAddr != "env-localhost:8080" {
		t.Errorf("Expected env server addr env-localhost:8080, got %s", cfg.ServerAddr)
	}
}
