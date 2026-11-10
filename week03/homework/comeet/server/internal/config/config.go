package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Env, Addr, Origin, DatabasePath                     string
	AllowedOrigins                                      []string
	JWTSecret, JWTIssuer, JWTAudience                   string
	JWTHours, MaxParticipants, EmptyMinutes, MaxMinutes int
	LivekitAPI, LivekitWS, LivekitKey, LivekitSecret    string
	AIEnabled                                           bool
	AIBase, AIModel, AIKey                              string
	AITimeout, AIMaxTokens                              int
}

func value(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
func Load(envFile string) (Config, error) {
	if envFile != "" {
		if err := godotenv.Load(envFile); err != nil {
			return Config{}, fmt.Errorf("load env file: %w", err)
		}
	}
	c := Config{Env: value("APP_ENV", "development"), Addr: value("HTTP_ADDR", "127.0.0.1:8080"), Origin: value("PUBLIC_ORIGIN", "http://localhost:5173"), DatabasePath: value("DATABASE_PATH", "./data/comeet.db"), JWTSecret: os.Getenv("JWT_SECRET"), JWTIssuer: value("JWT_ISSUER", "comeet"), JWTAudience: value("JWT_AUDIENCE", "comeet-web"), LivekitAPI: os.Getenv("LIVEKIT_API_URL"), LivekitWS: os.Getenv("LIVEKIT_WS_URL"), LivekitKey: os.Getenv("LIVEKIT_API_KEY"), LivekitSecret: os.Getenv("LIVEKIT_API_SECRET"), AIBase: value("DEEPSEEK_BASE_URL", "https://api.deepseek.com"), AIModel: os.Getenv("DEEPSEEK_MODEL"), AIKey: os.Getenv("DEEPSEEK_API_KEY")}
	for _, s := range strings.Split(value("ALLOWED_ORIGINS", c.Origin), ",") {
		c.AllowedOrigins = append(c.AllowedOrigins, strings.TrimSpace(s))
	}
	if _, _, err := net.SplitHostPort(c.Addr); err != nil {
		return c, fmt.Errorf("HTTP_ADDR must be host:port")
	}
	for _, s := range append([]string{c.Origin}, c.AllowedOrigins...) {
		u, e := url.Parse(s)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return c, fmt.Errorf("invalid public/allowed origin")
		}
	}
	for _, item := range []struct {
		k        string
		fallback int
		target   *int
	}{
		{"JWT_TTL_HOURS", 8, &c.JWTHours}, {"MAX_PARTICIPANTS", 6, &c.MaxParticipants}, {"MEETING_EMPTY_TIMEOUT_MINUTES", 10, &c.EmptyMinutes}, {"MEETING_MAX_DURATION_MINUTES", 240, &c.MaxMinutes}, {"DEEPSEEK_TIMEOUT_SECONDS", 90, &c.AITimeout}, {"DEEPSEEK_MAX_OUTPUT_TOKENS", 2048, &c.AIMaxTokens},
	} {
		n, e := strconv.Atoi(value(item.k, strconv.Itoa(item.fallback)))
		if e != nil || n < 1 {
			return c, fmt.Errorf("%s must be a positive integer", item.k)
		}
		*item.target = n
	}
	var err error
	c.AIEnabled, err = strconv.ParseBool(value("DEEPSEEK_ENABLED", "false"))
	if err != nil {
		return c, fmt.Errorf("DEEPSEEK_ENABLED must be boolean")
	}
	if len(c.JWTSecret) < 32 {
		return c, fmt.Errorf("JWT_SECRET must contain at least 32 bytes; run with -env-file .env -init-env first")
	}
	if c.MaxParticipants > 6 {
		return c, fmt.Errorf("MAX_PARTICIPANTS must be between 1 and 6")
	}
	return c, nil
}
func (c Config) MediaConfigured() bool {
	if c.LivekitKey == "" || c.LivekitSecret == "" {
		return false
	}
	api, a := url.Parse(c.LivekitAPI)
	ws, b := url.Parse(c.LivekitWS)
	if a != nil || b != nil || api.Host == "" || ws.Host == "" || api.User != nil || ws.User != nil || api.RawQuery != "" || ws.RawQuery != "" || api.Fragment != "" || ws.Fragment != "" {
		return false
	}
	if api.Scheme == "https" && ws.Scheme == "wss" {
		return true
	}
	// Cleartext is only for explicitly local development, never arbitrary LAN/public hosts.
	local := func(h string) bool { return h == "localhost" || h == "127.0.0.1" || h == "::1" }
	return c.Env == "development" && api.Scheme == "http" && ws.Scheme == "ws" && local(api.Hostname()) && local(ws.Hostname())
}
func (c Config) AIConfigured() bool { return c.AIEnabled && c.AIKey != "" && c.AIModel != "" }
