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

func TestSchedulerSecondOverridesAndLimits(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-only-32-byte-secret-000000000000")
	t.Setenv("MEETING_EMPTY_TIMEOUT_SECONDS", "2")
	t.Setenv("MEETING_MAX_DURATION_SECONDS", "8")
	t.Setenv("MEETING_SWEEP_SECONDS", "1")
	c, e := Load("")
	if e != nil || c.EmptySeconds != 2 || c.MaxSeconds != 8 || c.SweepSeconds != 1 {
		t.Fatal(c, e)
	}
	t.Setenv("MEETING_MAX_DURATION_SECONDS", "-1")
	if _, e = Load(""); e == nil {
		t.Fatal("negative timeout accepted")
	}
}

func TestDeepSeekConfigurationSafetyAndBudgets(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-only-32-byte-secret-000000000000")
	t.Setenv("DEEPSEEK_ENABLED", "true")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("DEEPSEEK_MODEL", "account-selected-model")
	t.Setenv("DEEPSEEK_BASE_URL", "https://api.deepseek.com")
	c, e := Load("")
	if e != nil || c.AIConfigured() {
		t.Fatal("missing key must not crash or be configured", e)
	}
	t.Setenv("DEEPSEEK_API_KEY", "local-test-key")
	c, e = Load("")
	if e != nil || !c.AIConfigured() || c.AIMaxSourceChars != 20000 || c.AIMaxSourceBytes != 80000 {
		t.Fatal(c.AIConfigured(), e)
	}
	for _, u := range []string{"https://user:secret@api.deepseek.com", "https://api.deepseek.com?key=x", "http://public.example", "https://api.deepseek.com#bad"} {
		t.Setenv("DEEPSEEK_BASE_URL", u)
		if _, e = Load(""); e == nil {
			t.Fatal("unsafe upstream allowed", u)
		}
	}
	t.Setenv("DEEPSEEK_BASE_URL", "https://api.deepseek.com")
	for _, k := range []string{"DEEPSEEK_TIMEOUT_SECONDS", "DEEPSEEK_MAX_OUTPUT_TOKENS", "DEEPSEEK_MAX_SOURCE_CHARS", "DEEPSEEK_MAX_SOURCE_BYTES"} {
		t.Setenv(k, "999999999")
		if _, e = Load(""); e == nil {
			t.Fatal("unbounded config", k)
		}
		t.Setenv(k, "")
	}
}
