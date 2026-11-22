package server

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/urmzd/jamalong/internal/room"
)

func TestLogChanges(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	before := room.Snapshot{NowItem: "a", NowTitle: "First", PositionMS: 29_700, DurationMS: 213_000, Lead: "Urmzd", Connected: 2, Speakers: 2}
	after := room.Snapshot{NowItem: "b", NowTitle: "Second", NowProvider: "soundcloud", NowBy: "Ada", Lead: "Ada", Connected: 2, Speakers: 1}
	logChanges(log, "r1", before, after, "lead Urmzd reported the end", "reason", "spotify-ended")
	out := buf.String()
	for _, want := range []string{
		`msg="track changed"`, "from=First", "from_at_ms=29700", "to=Second", "to_provider=soundcloud",
		"reason=spotify-ended", `msg="lead speaker changed"`, "speakers=1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q:\n%s", want, out)
		}
	}

	buf.Reset()
	logChanges(log, "r1", before, before, "vote by Ada")
	if buf.Len() != 0 {
		t.Errorf("no change logged %q", buf.String())
	}
	paused := before
	paused.Paused = true
	logChanges(log, "r1", before, paused, "pause by Ada")
	if !strings.Contains(buf.String(), `msg="playback paused"`) {
		t.Errorf("pause not logged: %q", buf.String())
	}
}

func TestReasonText(t *testing.T) {
	for in, want := range map[string]string{
		"spotify-ended":          "spotify-ended",
		"YouTube-Error":          "youtube-error",
		"bad\nline=x \"quote\"":  "badlinexquote",
		strings.Repeat("a", 100): strings.Repeat("a", 40),
	} {
		if got := reasonText(in); got != want {
			t.Errorf("reasonText(%q) = %q, want %q", in, got, want)
		}
	}
}
