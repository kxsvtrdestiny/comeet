package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"github.com/joho/godotenv"
	"os"
	"strings"
)

// InitializeEnv writes a private local key, retaining existing settings/comments.
// A nonempty key is never rotated automatically: persisted JWTs need the same key.
func InitializeEnv(file string) error {
	if file == "" {
		return fmt.Errorf("-init-env requires -env-file .env")
	}
	body, e := os.ReadFile(file)
	if os.IsNotExist(e) {
		body, e = os.ReadFile(".env.example")
	}
	if e != nil {
		return e
	}
	values, e := godotenv.Unmarshal(string(body))
	if e != nil {
		return fmt.Errorf("invalid dotenv file")
	}
	if value := values["JWT_SECRET"]; value != "" {
		if len(value) < 32 {
			return fmt.Errorf("existing JWT_SECRET is too short; choose a private key of at least 32 bytes")
		}
		return nil
	}
	secret := make([]byte, 32)
	if _, e = rand.Read(secret); e != nil {
		return e
	}
	line := "JWT_SECRET=" + base64.RawURLEncoding.EncodeToString(secret)
	lines := strings.Split(string(body), "\n")
	found := false
	for i, v := range lines {
		head := strings.TrimSpace(strings.SplitN(v, "=", 2)[0])
		if head == "JWT_SECRET" || head == "export JWT_SECRET" {
			lines[i] = line
			found = true
		}
	}
	if !found {
		lines = append(lines, line)
	}
	return os.WriteFile(file, []byte(strings.Join(lines, "\n")), 0600)
}
