package httpapi

import (
	"comeet/server/internal/realtime"
	"encoding/json"
	"github.com/golang-jwt/jwt/v5"
	"strings"
	"testing"
	"time"
)

func TestMediaTokenBoundToOnlineSessionAndMinimalGrants(t *testing.T) {
	h := newWSHarness(t)
	h.hub.Config.LivekitAPI = "https://media.example.test"
	h.hub.Config.LivekitWS = "wss://media.example.test"
	h.hub.Config.LivekitKey = "test-media-key"
	h.hub.Config.LivekitSecret = "test-private-media-secret-never-expose"
	a, b := h.user("MediaHost"), h.user("MediaGuest")
	m := h.create(a.Token, "音视频权限")
	ra := h.reserve(a.Token, m.ID)
	path := "/meetings/" + m.ID + "/media-token"
	s, r := h.call("POST", path, a.Token, realtime.SessionInput{SessionID: ra.SessionID})
	check(t, s, r, 409, "BUSINESS_NOT_CONNECTED")
	ca := h.connect(a.Token, m.ID, ra.SessionID)
	s, r = h.call("POST", path, b.Token, realtime.SessionInput{SessionID: ra.SessionID})
	check(t, s, r, 403, "FORBIDDEN")
	other := h.create(a.Token, "不同会议")
	s, r = h.call("POST", "/meetings/"+other.ID+"/media-token", a.Token, realtime.SessionInput{SessionID: ra.SessionID})
	check(t, s, r, 403, "FORBIDDEN")
	s, r = h.call("POST", path, a.Token, map[string]any{"sessionId": ra.SessionID, "identity": "host", "roomAdmin": true})
	check(t, s, r, 400, "VALIDATION_ERROR")
	s, r = h.call("POST", path, a.Token, realtime.SessionInput{SessionID: ra.SessionID})
	check(t, s, r, 200, "OK")
	result := unpack[realtime.MediaResult](t, r)
	token, e := jwt.Parse(result.Token, func(token *jwt.Token) (any, error) { return []byte(h.hub.Config.LivekitSecret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(h.hub.Config.LivekitKey), jwt.WithSubject(ra.SessionID))
	if e != nil || !token.Valid {
		t.Fatal("invalid media JWT", e)
	}
	claims := token.Claims.(jwt.MapClaims)
	g := claims["video"].(map[string]any)
	if g["room"] != m.ID || g["roomJoin"] != true || g["canSubscribe"] != true || g["canPublishData"] != false || g["canUpdateOwnMetadata"] != false || g["roomAdmin"] != nil || g["roomCreate"] != nil {
		t.Fatal("excess grant", g)
	}
	sources := g["canPublishSources"].([]any)
	if len(sources) != 3 || sources[0] != "camera" || sources[1] != "microphone" || sources[2] != "screen_share" {
		t.Fatal("unexpected source/admin permission granted")
	}
	exp, _ := claims.GetExpirationTime()
	if exp.Time.After(time.Now().Add(5*time.Minute)) || !exp.Time.After(time.Now()) {
		t.Fatal("bad token TTL")
	}

	// A shorter valid JWT for the same login must cap the media grant too.
	shortClaims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(a.Token, shortClaims, func(*jwt.Token) (any, error) { return []byte(h.c.JWTSecret), nil })
	if err != nil {
		t.Fatal(err)
	}
	shortClaims["exp"] = time.Now().Add(45 * time.Second).Unix()
	shortToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, shortClaims).SignedString([]byte(h.c.JWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	st, rr := h.call("POST", path, shortToken, realtime.SessionInput{SessionID: ra.SessionID})
	check(t, st, rr, 200, "OK")
	shortGrant := unpack[realtime.MediaResult](t, rr)
	if shortGrant.ExpiresAt > time.Now().Add(45*time.Second).UnixMilli() {
		t.Fatal("media outlives the requesting JWT")
	}
	var metadata map[string]string
	if json.Unmarshal([]byte(claims["metadata"].(string)), &metadata) != nil || metadata["userId"] != a.User.ID || metadata["sessionId"] != ra.SessionID {
		t.Fatal("bad metadata")
	}
	if strings.Contains(string(r.Data), h.hub.Config.LivekitSecret) || result.URL != h.hub.Config.LivekitWS || result.Identity != ra.SessionID || result.Room != m.ID {
		t.Fatal("media response exposed secret or wrong binding")
	}
	h.db.Exec("UPDATE meetings SET status='ending' WHERE id=?", m.ID)
	s, r = h.call("POST", path, a.Token, realtime.SessionInput{SessionID: ra.SessionID})
	check(t, s, r, 409, "MEETING_ENDING")
	h.db.Exec("UPDATE meetings SET status='active' WHERE id=?", m.ID)
	if _, e := h.db.Exec("INSERT INTO meeting_bans(meeting_id,user_id,created_by,created_at) VALUES (?,?,?,?)", m.ID, a.User.ID, a.User.ID, time.Now().UnixMilli()); e != nil {
		t.Fatal(e)
	}
	s, r = h.call("POST", path, a.Token, realtime.SessionInput{SessionID: ra.SessionID})
	check(t, s, r, 403, "REMOVED_FROM_MEETING")
	h.db.Exec("DELETE FROM meeting_bans WHERE meeting_id=?", m.ID)
	s, r = h.call("POST", "/meetings/"+m.ID+"/leave", a.Token, realtime.SessionInput{SessionID: ra.SessionID})
	check(t, s, r, 200, "OK")
	s, r = h.call("POST", path, a.Token, realtime.SessionInput{SessionID: ra.SessionID})
	check(t, s, r, 409, "SESSION_EXPIRED")
	_ = ca
}
func TestMediaMissingConfigurationDoesNotBreakChat(t *testing.T) {
	h := newWSHarness(t)
	a := h.user("MediaOptional")
	m := h.create(a.Token, "文字会议")
	ra := h.reserve(a.Token, m.ID)
	ca := h.connect(a.Token, m.ID, ra.SessionID)
	s, r := h.call("POST", "/meetings/"+m.ID+"/media-token", a.Token, realtime.SessionInput{SessionID: ra.SessionID})
	check(t, s, r, 503, "MEDIA_NOT_CONFIGURED")
	ca.send("ping", map[string]any{})
	ca.wait("pong")
	s, r = h.call("POST", "/auth/logout", a.Token, nil)
	check(t, s, r, 200, "OK")
	s, r = h.call("POST", "/meetings/"+m.ID+"/media-token", a.Token, realtime.SessionInput{SessionID: ra.SessionID})
	check(t, s, r, 401, "SESSION_EXPIRED")
}
