package auth

import (
	"comeet/server/internal/apperr"
	"comeet/server/internal/config"
	"context"
	"database/sql"
	"errors"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}
type Identity struct {
	User User
	SID  string
}
type Claims struct {
	SID string `json:"sid"`
	jwt.RegisteredClaims
}
type Service struct {
	DB     *sql.DB
	Config config.Config
}
type RegisterInput struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
}
type LoginInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
type LoginResult struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expiresAt"`
	User      User   `json:"user"`
}

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,32}$`)

// A constant dummy hash equalizes the unknown-user bcrypt path; never a usable account.
var dummyHash = []byte("$2a$10$7EqJtq98hPqEX7fNZaFWoO5f6ilzYi0vJ5zGXuYvkJ7p3XF1JtqvK")

func (s *Service) Register(ctx context.Context, in RegisterInput) (User, error) {
	in.Username = strings.TrimSpace(in.Username)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if !usernamePattern.MatchString(in.Username) {
		return User{}, apperr.Invalid("用户名需为 3–32 位字母、数字或下划线")
	}
	if n := utf8.RuneCountInString(in.DisplayName); n < 1 || n > 32 {
		return User{}, apperr.Invalid("显示名称需为 1–32 个字符")
	}
	if utf8.RuneCountInString(in.Password) < 8 || len(in.Password) > 72 {
		return User{}, apperr.Invalid("密码至少 8 个字符，UTF-8 长度不超过 72 字节")
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if e != nil {
		return User{}, e
	}
	u := User{uuid.NewString(), in.Username, in.DisplayName}
	_, e = s.DB.ExecContext(ctx, "INSERT INTO users(id,username,normalized_username,display_name,password_hash,created_at) VALUES (?,?,?,?,?,?)", u.ID, u.Username, strings.ToLower(u.Username), u.DisplayName, string(hash), time.Now().UnixMilli())
	if apperr.IsUnique(e) {
		return User{}, apperr.New(409, "USERNAME_TAKEN", "用户名已被使用")
	}
	return u, e
}
func (s *Service) Login(ctx context.Context, in LoginInput) (LoginResult, error) {
	var u User
	var hash string
	e := s.DB.QueryRowContext(ctx, "SELECT id,username,display_name,password_hash FROM users WHERE normalized_username=?", strings.ToLower(strings.TrimSpace(in.Username))).Scan(&u.ID, &u.Username, &u.DisplayName, &hash)
	if errors.Is(e, sql.ErrNoRows) {
		bcrypt.CompareHashAndPassword(dummyHash, []byte(in.Password))
		return LoginResult{}, apperr.New(401, "UNAUTHORIZED", "用户名或密码错误")
	}
	if e != nil {
		return LoginResult{}, e
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)) != nil {
		return LoginResult{}, apperr.New(401, "UNAUTHORIZED", "用户名或密码错误")
	}
	now := time.Now().UTC()
	expires := now.Add(time.Duration(s.Config.JWTHours) * time.Hour).Truncate(time.Second)
	sid := uuid.NewString()
	claims := Claims{sid, jwt.RegisteredClaims{Subject: u.ID, Issuer: s.Config.JWTIssuer, Audience: jwt.ClaimStrings{s.Config.JWTAudience}, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(expires)}}
	token, e := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.Config.JWTSecret))
	if e != nil {
		return LoginResult{}, e
	}
	if _, e = s.DB.ExecContext(ctx, "INSERT INTO auth_sessions(id,user_id,expires_at,created_at) VALUES (?,?,?,?)", sid, u.ID, expires.UnixMilli(), now.UnixMilli()); e != nil {
		return LoginResult{}, e
	}
	return LoginResult{token, expires.UnixMilli(), u}, nil
}
func (s *Service) Authenticate(ctx context.Context, raw string) (Identity, error) {
	claims := &Claims{}
	_, e := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) { return []byte(s.Config.JWTSecret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(s.Config.JWTIssuer), jwt.WithAudience(s.Config.JWTAudience), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if e != nil {
		if errors.Is(e, jwt.ErrTokenExpired) {
			return Identity{}, apperr.New(401, "SESSION_EXPIRED", "登录已过期，请重新登录")
		}
		return Identity{}, apperr.New(401, "UNAUTHORIZED", "登录凭证无效，请重新登录")
	}
	if claims.Subject == "" || claims.SID == "" || claims.IssuedAt == nil {
		return Identity{}, apperr.New(401, "UNAUTHORIZED", "登录凭证无效，请重新登录")
	}
	var u User
	e = s.DB.QueryRowContext(ctx, `SELECT u.id,u.username,u.display_name FROM users u JOIN auth_sessions a ON a.user_id=u.id WHERE a.id=? AND a.user_id=? AND a.revoked_at IS NULL AND a.expires_at>?`, claims.SID, claims.Subject, time.Now().UnixMilli()).Scan(&u.ID, &u.Username, &u.DisplayName)
	if errors.Is(e, sql.ErrNoRows) {
		return Identity{}, apperr.New(401, "SESSION_EXPIRED", "登录已失效，请重新登录")
	}
	if e != nil {
		return Identity{}, e
	}
	return Identity{u, claims.SID}, nil
}
func (s *Service) Logout(ctx context.Context, id Identity) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	if _, e = tx.ExecContext(ctx, "UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,?) WHERE id=? AND user_id=?", now, id.SID, id.User.ID); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE participant_sessions SET state='left',left_at=? WHERE auth_session_id=? AND state='joining'", now, id.SID); e != nil {
		return e
	}
	return tx.Commit()
}
