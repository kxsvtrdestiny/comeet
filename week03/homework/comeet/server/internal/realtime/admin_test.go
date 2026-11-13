package realtime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"comeet/server/internal/config"
	"github.com/golang-jwt/jwt/v5"
	"github.com/livekit/protocol/livekit"
	"google.golang.org/protobuf/proto"
)

func TestOfficialSDKRequestsMicOnlyAndExplicitRevocation(t *testing.T) {
	var muted bool
	var cutoff int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		claims := jwt.MapClaims{}
		if _, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) { return []byte("test-secret"), nil }); err != nil {
			t.Error("bad signed admin token", err)
		}
		raw, _ := io.ReadAll(r.Body)
		var out proto.Message
		switch {
		case strings.HasSuffix(r.URL.Path, "/GetParticipant"):
			in := &livekit.RoomParticipantIdentity{}
			if err := proto.Unmarshal(raw, in); err != nil {
				t.Error(err)
			}
			if in.Room != "room" || in.Identity != "target-session" {
				t.Error("wrong identity", in)
			}
			out = &livekit.ParticipantInfo{Tracks: []*livekit.TrackInfo{{Sid: "camera", Source: livekit.TrackSource_CAMERA}, {Sid: "microphone", Source: livekit.TrackSource_MICROPHONE}}}
		case strings.HasSuffix(r.URL.Path, "/MutePublishedTrack"):
			in := &livekit.MuteRoomTrackRequest{}
			if err := proto.Unmarshal(raw, in); err != nil {
				t.Error(err)
			}
			if in.TrackSid != "microphone" || !in.Muted {
				t.Error("must only mute microphone", in)
			}
			muted = true
			out = &livekit.MuteRoomTrackResponse{}
		case strings.HasSuffix(r.URL.Path, "/RemoveParticipant"):
			in := &livekit.RoomParticipantIdentity{}
			if err := proto.Unmarshal(raw, in); err != nil {
				t.Error(err)
			}
			cutoff = in.RevokeTokenTs
			out = &livekit.RemoveParticipantResponse{}
		case strings.HasSuffix(r.URL.Path, "/DeleteRoom"):
			out = &livekit.DeleteRoomResponse{}
		default:
			t.Error("unexpected method", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		bytes, err := proto.Marshal(out)
		if err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/protobuf")
		w.Write(bytes)
	}))
	defer server.Close()
	s := New(nil, config.Config{Env: "development", LivekitAPI: server.URL, LivekitWS: strings.Replace(server.URL, "http:", "ws:", 1), LivekitKey: "test-key", LivekitSecret: "test-secret"})
	admin, err := s.admin()
	if err != nil {
		t.Fatal(err)
	}
	if err = admin.Mute(context.Background(), "room", "target-session"); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Unix() + 1
	if err = admin.Remove(context.Background(), "room", "target-session", at); err != nil {
		t.Fatal(err)
	}
	if err = admin.Delete(context.Background(), "room"); err != nil {
		t.Fatal(err)
	}
	if !muted || cutoff != at {
		t.Fatal("missing mute or revocation cutoff", muted, cutoff)
	}
}
