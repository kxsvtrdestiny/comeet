package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsAndValidation(t *testing.T) {
	t.Setenv("HTTP_ADDR", "127.0.0.1:8080")
	t.Setenv("MAX_PARTICIPANTS", "6")
	c, e := Load("")
	if e != nil {
		t.Fatal(e)
	}
	if c.MediaConfigured() || c.AIConfigured() {
		t.Fatal("unexpected optional services")
	}
	t.Setenv("MAX_PARTICIPANTS", "bad")
	if _, e = Load(""); e == nil {
		t.Fatal("must reject invalid configuration")
	}
}
func TestExplicitEnvFile(t *testing.T) {
	t.Setenv("HTTP_ADDR", "127.0.0.1:9090")
	t.Setenv("MAX_PARTICIPANTS", "6")
	p := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(p, []byte("HTTP_ADDR=127.0.0.1:8888\n"), 0600)
	c, e := Load(p)
	if e != nil || c.Addr != "127.0.0.1:9090" {
		t.Fatal("process env must win", e)
	}
	if _, e = Load(p + "missing"); e == nil {
		t.Fatal("missing explicit file must fail")
	}
}
