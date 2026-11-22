package server

import (
	"log/slog"
	"strings"

	"github.com/urmzd/jamalong/internal/room"
)

// logChanges writes one line for each thing that changed between two
// snapshots of a room: the track, play or pause, the lead speaker, the
// number of speakers. cause names what changed it, for example "skip by Ada"
// or "stall timer".
func logChanges(log *slog.Logger, roomID string, before, after room.Snapshot, cause string, extra ...any) {
	base := []any{"room", roomID, "cause", cause}
	if before.NowItem != after.NowItem {
		attrs := append(append([]any{}, base...),
			"from", before.NowTitle,
			"from_at_ms", before.PositionMS,
			"from_duration_ms", before.DurationMS,
			"to", after.NowTitle,
			"to_provider", after.NowProvider,
			"to_added_by", after.NowBy,
			"queue_left", after.Queue,
		)
		log.Info("track changed", append(attrs, extra...)...)
	} else if before.Paused != after.Paused {
		state := "playing"
		if after.Paused {
			state = "paused"
		}
		log.Info("playback "+state, append(append(base, "track", after.NowTitle, "at_ms", after.PositionMS), extra...)...)
	}
	if before.Lead != after.Lead {
		log.Info("lead speaker changed", append(base, "from", before.Lead, "to", after.Lead)...)
	}
	if before.Speakers != after.Speakers || before.Connected != after.Connected {
		log.Info("members changed", append(base, "connected", after.Connected, "speakers", after.Speakers)...)
	}
}

// reasonText keeps a client-sent reason short and printable.
func reasonText(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, strings.ToLower(s))
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}
