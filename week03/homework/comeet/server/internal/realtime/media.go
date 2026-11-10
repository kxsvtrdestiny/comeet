package realtime

import (
	"context"
	"encoding/json"
	"time"

	"comeet/server/internal/apperr"
	"comeet/server/internal/auth"
	"github.com/golang-jwt/jwt/v5"
)

// LiveKit's documented HS256 access-token contract. No administration or data grant.
// Identity is the server-owned participant session, never client-provided user/role.
type MediaResult struct {
	URL       string `json:"url"`
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expiresAt"`
	Identity  string `json:"identity"`
	Room      string `json:"room"`
}

func (s *Service) MediaToken(ctx context.Context, id auth.Identity, meetingID string, in SessionInput) (MediaResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, e := s.load(ctx, in.SessionID)
	if e != nil {
		return MediaResult{}, e
	}
	now := time.Now()
	if e = s.allowed(v, id, meetingID, now.UnixMilli()); e != nil {
		return MediaResult{}, e
	}
	p := s.peers[v.ID]
	if v.State != "online" || !v.JoinedAt.Valid || p == nil || p.ctx.Err() != nil || p.generation != v.Generation {
		return MediaResult{}, apperr.New(409, "BUSINESS_NOT_CONNECTED", "请先完成业务入会，再连接音视频")
	}
	if !s.Config.MediaConfigured() {
		return MediaResult{}, apperr.New(503, "MEDIA_NOT_CONFIGURED", "尚未配置 LiveKit，可继续文字会议")
	}
	expiry := now.Add(5 * time.Minute)
	if v.AuthExpiry < expiry.UnixMilli() {
		expiry = time.UnixMilli(v.AuthExpiry)
	}
	if id.ExpiresAt > 0 && id.ExpiresAt < expiry.UnixMilli() {
		expiry = time.UnixMilli(id.ExpiresAt)
	}
	metadata, _ := json.Marshal(map[string]string{"userId": v.UserID, "sessionId": v.ID})
	claims := jwt.MapClaims{
		"iss": s.Config.LivekitKey, "sub": v.ID, "name": p.name, "metadata": string(metadata),
		"iat": now.Unix(), "nbf": now.Add(-5 * time.Second).Unix(), "exp": expiry.Unix(),
		"video": map[string]any{"roomJoin": true, "room": meetingID, "canPublish": true, "canSubscribe": true, "canPublishData": false, "canUpdateOwnMetadata": false, "canPublishSources": []string{"camera", "microphone"}},
	}
	token, e := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.Config.LivekitSecret))
	if e != nil {
		return MediaResult{}, e
	}
	return MediaResult{s.Config.LivekitWS, token, expiry.UnixMilli(), v.ID, meetingID}, nil
}
