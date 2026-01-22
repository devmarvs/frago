package caddy

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pickFreePort(t *testing.T) int {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return port
}

func TestEnsureCaddyfile_UsesDesiredPort(t *testing.T) {
	dir := t.TempDir()
	desired := pickFreePort(t)

	cfg, err := EnsureCaddyfile(dir, nil, desired)
	if err != nil {
		t.Fatalf("EnsureCaddyfile returned error: %v", err)
	}
	if cfg.Port != desired {
		t.Fatalf("expected port %d, got %d", desired, cfg.Port)
	}

	data, err := os.ReadFile(filepath.Join(dir, "Caddyfile"))
	if err != nil {
		t.Fatalf("read Caddyfile: %v", err)
	}
	if !strings.Contains(string(data), fmt.Sprintf(":%d", desired)) {
		t.Fatalf("Caddyfile does not include desired port %d", desired)
	}
}

func TestEnsureCaddyfile_DesiredPortInUse(t *testing.T) {
	dir := t.TempDir()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for port: %v", err)
	}
	t.Cleanup(func() {
		_ = ln.Close()
	})
	inUsePort := ln.Addr().(*net.TCPAddr).Port

	_, err = EnsureCaddyfile(dir, nil, inUsePort)
	if err == nil {
		t.Fatalf("expected error for port in use")
	}
	if !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("expected port in use error, got: %v", err)
	}
}
