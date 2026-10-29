package telegram

import (
	"context"
	"errors"

	"github.com/oniharnantyo/buddies/internal/store"
)

// excerptMax bounds the announcement excerpt the scheduler side extracts
// (design D13's ≤400 chars; the announcer clips defensively anyway).
const excerptMax = 400

// AnnounceRunCompletion delivers one schedule run's outcome to the home chat
// (design D13): "<schedule name> — <outcome> — <excerpt>", through the normal
// outbound path (pacing, chunking, flood rules inherited). A missing home
// chat or a disabled channel is a no-op; store and send errors return — the
// caller logs them and never touches RecordFire outcomes.
func (s *Service) AnnounceRunCompletion(ctx context.Context, scheduleName, outcome, excerpt string) error {
	row, err := s.chans.GetChannel(ctx, s.channelID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil // no channel configured: nothing to announce to
		}
		return wrapStore("read channel for announcement", err)
	}
	if !row.Enabled {
		return nil // disabled channel: no-op (design D13)
	}

	home, err := s.homeChat(ctx)
	if err != nil {
		return err
	}
	if home == nil {
		return nil // no home chat designated: no-op
	}

	text := scheduleName + " — " + outcome
	if excerpt != "" {
		text += " — " + truncate(excerpt, excerptMax)
	}
	if err := s.sendMarkdown(ctx, home.ChatID, text, nil); err != nil {
		return err
	}
	return nil
}

// homeChat returns the single home chat row, nil when none is designated.
func (s *Service) homeChat(ctx context.Context) (*store.ChannelChat, error) {
	chats, err := s.chans.ListChannelChats(ctx)
	if err != nil {
		return nil, wrapStore("list chats for announcement", err)
	}
	for i := range chats {
		if chats[i].IsHome {
			home := chats[i]
			return &home, nil
		}
	}
	return nil, nil
}
