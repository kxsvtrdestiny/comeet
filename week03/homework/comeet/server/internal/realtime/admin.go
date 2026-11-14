package realtime

import (
	"context"
	"errors"
	"time"

	"comeet/server/internal/apperr"
	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/twitchtv/twirp"
)

// Narrow seam for deterministic tests; production uses the official Go RoomService SDK.
type MediaAdmin interface {
	List(context.Context, string) ([]string, error)
	Mute(context.Context, string, string) error
	Remove(context.Context, string, string, int64) error
	Delete(context.Context, string) error
}
type livekitAdmin struct{ client *lksdk.RoomServiceClient }

func absent(err error) bool {
	var e twirp.Error
	return errors.As(err, &e) && e.Code() == twirp.NotFound
}
func (a livekitAdmin) List(ctx context.Context, room string) ([]string, error) {
	result, err := a.client.ListParticipants(ctx, &livekit.ListParticipantsRequest{Room: room})
	if absent(err) {
		logAbsent(ctx, "list_participants")
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(result.Participants))
	for _, p := range result.Participants {
		ids = append(ids, p.Identity)
	}
	return ids, nil
}
func (a livekitAdmin) Mute(ctx context.Context, room, identity string) error {
	p, err := a.client.GetParticipant(ctx, &livekit.RoomParticipantIdentity{Room: room, Identity: identity})
	if err != nil {
		if absent(err) {
			return apperr.New(409, "MICROPHONE_NOT_PUBLISHED", "该成员当前未开麦")
		}
		return err
	}
	for _, t := range p.Tracks {
		if t.Source == livekit.TrackSource_MICROPHONE && !t.Muted {
			_, err = a.client.MutePublishedTrack(ctx, &livekit.MuteRoomTrackRequest{Room: room, Identity: identity, TrackSid: t.Sid, Muted: true})
			return err
		}
	}
	return apperr.New(409, "MICROPHONE_NOT_PUBLISHED", "该成员当前未开麦")
}
func (a livekitAdmin) Remove(ctx context.Context, room, identity string, cutoff int64) error {
	_, err := a.client.RemoveParticipant(ctx, &livekit.RoomParticipantIdentity{Room: room, Identity: identity, RevokeTokenTs: cutoff})
	// Repeated removal is complete when the upstream explicitly reports no participant.
	if absent(err) {
		logAbsent(ctx, "remove_participant")
		return nil
	}
	return err
}
func (a livekitAdmin) Delete(ctx context.Context, room string) error {
	_, err := a.client.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: room})
	if absent(err) {
		logAbsent(ctx, "delete_room")
		return nil
	}
	return err
}
func (s *Service) admin() (MediaAdmin, error) {
	if s.MediaAdmin != nil {
		return s.MediaAdmin, nil
	}
	if !s.Config.MediaConfigured() {
		return nil, apperr.New(503, "MEDIA_NOT_CONFIGURED", "媒体管理尚未配置，操作未完全完成，请配置后重试")
	}
	return livekitAdmin{lksdk.NewRoomServiceClient(s.Config.LivekitAPI, s.Config.LivekitKey, s.Config.LivekitSecret)}, nil
}
func mediaTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 6*time.Second)
}
