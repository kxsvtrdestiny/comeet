package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultsAndValidation(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-only-32-byte-secret-000000000000")
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
	t.Setenv("JWT_SECRET", "test-only-32-byte-secret-000000000000")
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

func TestPrivateKeyInitialization(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	original := "# user settings retained\nHTTP_ADDR=127.0.0.1:9090\nJWT_SECRET=\nDATABASE_PATH=./custom/test.db\n"
	if e := os.WriteFile(p, []byte(original), 0600); e != nil {
		t.Fatal(e)
	}
	if e := InitializeEnv(p); e != nil {
		t.Fatal(e)
	}
	first, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(first), "# user settings retained") || !strings.Contains(string(first), "DATABASE_PATH=./custom/test.db") {
		t.Fatal("existing settings overwritten")
	}
	if e = InitializeEnv(p); e != nil {
		t.Fatal(e)
	}
	again, _ := os.ReadFile(p)
	if string(first) != string(again) {
		t.Fatal("key rotated on repeated initialization")
	}
	t.Setenv("JWT_SECRET", "")
	if _, e = Load(""); e == nil {
		t.Fatal("missing JWT secret must fail")
	}
	t.Setenv("JWT_SECRET", "short")
	if _, e = Load(""); e == nil {
		t.Fatal("weak JWT secret must fail")
	}
	t.Setenv("JWT_SECRET", "test-only-long-enough-secret-0123456789")
	t.Setenv("MAX_PARTICIPANTS", "7")
	if _, e = Load(""); e == nil {
		t.Fatal("product capacity limit exceeded")
	}
}

func TestMediaConfigurationValidation(t *testing.T) {
	c := Config{Env: "development", LivekitAPI: "https://project.livekit.cloud", LivekitWS: "wss://project.livekit.cloud", LivekitKey: "key", LivekitSecret: "secret"}
	if !c.MediaConfigured() {
		t.Fatal("cloud config rejected")
	}
	c.LivekitWS = "ws://public.example.test"
	if c.MediaConfigured() {
		t.Fatal("insecure public media allowed")
	}
	c.LivekitAPI = "http://localhost:7880"
	c.LivekitWS = "ws://localhost:7880"
	if !c.MediaConfigured() {
		t.Fatal("local development rejected")
	}
	c.Env = "production"
	if c.MediaConfigured() {
		t.Fatal("cleartext production allowed")
	}
	c.Env = "development"
	c.LivekitWS = "ws://localhost:7880?secret=bad"
	if c.MediaConfigured() {
		t.Fatal("query credentials allowed")
	}
	c.LivekitWS = "ws://localhost:7880"
	c.LivekitSecret = ""
	if c.MediaConfigured() {
		t.Fatal("partial config allowed")
	}
}
