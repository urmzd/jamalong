package room

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 21, 20, 0, 0, 0, time.UTC)

func at(d time.Duration) time.Time { return t0.Add(d) }

func track(title string) Track {
	return Track{Provider: "youtube", ProviderID: "id-" + title, Title: title, DurationMS: 180_000}
}

// party returns a room whose host device is a connected speaker, plus n
// connected guests.
func party(t *testing.T, n int) (*Room, string, []string) {
	t.Helper()
	r, host := New("room", "Host")
	must(t, r.Connect(host, t0))
	must(t, r.SetSpeaker(host, true, t0))
	guests := make([]string, n)
	for i := range guests {
		id, err := r.Join("Guest")
		must(t, err)
		must(t, r.Connect(id, t0))
		guests[i] = id
	}
	return r, host, guests
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func add(t *testing.T, r *Room, by, title string) string {
	t.Helper()
	id, err := r.Add(by, track(title), t0)
	must(t, err)
	return id
}

func titles(r *Room, viewer string) string {
	v := r.View(viewer)
	out := []string{}
	if v.Now != nil {
		out = append(out, "["+v.Now.Item.Track.Title+"]")
	}
	for _, it := range v.Queue {
		out = append(out, it.Track.Title)
	}
	return strings.Join(out, " ")
}

func TestQueueIsFairByPerson(t *testing.T) {
	r, host, g := party(t, 2)
	a, b := g[0], g[1]
	for _, s := range []string{"a1", "a2", "a3"} {
		add(t, r, a, s)
	}
	add(t, r, b, "b1")
	add(t, r, host, "h1")
	add(t, r, b, "b2")

	// a1 started at once. Then one track per person per round.
	if got, want := titles(r, host), "[a1] b1 h1 a2 b2 a3"; got != want {
		t.Fatalf("order = %q, want %q", got, want)
	}

	// A member already served in the playing round waits for the next one.
	must(t, r.Skip(host, t0)) // now b1, still round 0
	add(t, r, a, "a4")
	if got, want := titles(r, host), "[b1] h1 a2 b2 a3 a4"; got != want {
		t.Fatalf("order = %q, want %q", got, want)
	}
}

func TestVotesOrderWithinARound(t *testing.T) {
	r, host, g := party(t, 3)
	add(t, r, host, "h1")
	add(t, r, g[0], "a1")
	b1 := add(t, r, g[1], "b1")
	add(t, r, g[2], "c1")

	must(t, r.Vote(g[0], b1))
	if got, want := titles(r, host), "[h1] b1 a1 c1"; got != want {
		t.Fatalf("order = %q, want %q", got, want)
	}
	must(t, r.Vote(g[0], b1)) // toggles off
	if got, want := titles(r, host), "[h1] a1 b1 c1"; got != want {
		t.Fatalf("order = %q, want %q", got, want)
	}
	if err := r.Vote(g[1], b1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("own vote err = %v, want ErrInvalid", err)
	}
}

func TestGuestsSkipByMajority(t *testing.T) {
	r, host, g := party(t, 2) // three connected members: two votes skip
	add(t, r, host, "h1")
	add(t, r, g[0], "a1")

	must(t, r.Skip(g[0], t0))
	if v := r.View(g[0]); v.Now.Item.Track.Title != "h1" || v.Now.SkipVotes != 1 || v.Now.SkipNeeded != 2 || !v.Now.SkipVoted {
		t.Fatalf("after one vote: %+v", v.Now)
	}
	must(t, r.Skip(g[1], t0))
	if v := r.View(host); v.Now.Item.Track.Title != "a1" || v.Now.SkipVotes != 0 {
		t.Fatalf("after majority: %+v", v.Now)
	}
}

func TestRolesGateCommands(t *testing.T) {
	r, host, g := party(t, 2)
	guest, other := g[0], g[1]
	add(t, r, host, "h1")
	mine := add(t, r, guest, "a1")
	theirs := add(t, r, other, "b1")

	forbidden := map[string]error{
		"pause":    r.Pause(guest, t0),
		"play":     r.Play(guest, t0),
		"seek":     r.Seek(guest, 1000, t0),
		"remove":   r.Remove(guest, theirs),
		"set_role": r.SetRole(guest, other, RoleCollaborator),
		"kick":     r.Kick(guest, other, t0),
		"end":      r.End(guest),
	}
	for name, err := range forbidden {
		if !errors.Is(err, ErrForbidden) {
			t.Errorf("guest %s err = %v, want ErrForbidden", name, err)
		}
	}
	must(t, r.Remove(guest, mine))

	must(t, r.SetRole(host, guest, RoleCollaborator))
	must(t, r.Pause(guest, t0))
	must(t, r.Remove(guest, theirs))
	if err := r.Kick(guest, other, t0); !errors.Is(err, ErrForbidden) {
		t.Errorf("collaborator kick err = %v, want ErrForbidden", err)
	}
	if err := r.SetRole(host, guest, RoleHost); !errors.Is(err, ErrInvalid) {
		t.Errorf("second host err = %v, want ErrInvalid", err)
	}
}

func TestClockPauseResumeSeek(t *testing.T) {
	r, host, _ := party(t, 0)
	add(t, r, host, "h1")

	pos := func(d time.Duration) time.Duration {
		p, ok := r.Position(at(d))
		if !ok {
			t.Fatal("nothing playing")
		}
		return p
	}
	if got := pos(10 * time.Second); got != 10*time.Second {
		t.Fatalf("playing position = %v", got)
	}
	must(t, r.Pause(host, at(10*time.Second)))
	if got := pos(25 * time.Second); got != 10*time.Second {
		t.Fatalf("paused position = %v", got)
	}
	must(t, r.Play(host, at(30*time.Second)))
	if got := pos(35 * time.Second); got != 15*time.Second {
		t.Fatalf("resumed position = %v", got)
	}
	must(t, r.Seek(host, 60_000, at(35*time.Second)))
	if got := pos(36 * time.Second); got != 61*time.Second {
		t.Fatalf("seek position = %v", got)
	}
	must(t, r.Seek(host, 999_999_999, at(36*time.Second)))
	if got := pos(36 * time.Second); got != 180*time.Second {
		t.Fatalf("seek past the end = %v, want clamp to duration", got)
	}
}

func TestRoomWaitsWithoutASpeaker(t *testing.T) {
	r, host := New("room", "Host")
	must(t, r.Connect(host, t0))
	add(t, r, host, "h1")
	if v := r.View(host); v.Now == nil || !v.Now.Waiting || v.Lead != "" {
		t.Fatalf("no speaker: now = %+v lead = %q", v.Now, v.Lead)
	}
	if err := r.Play(host, t0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("play without speaker err = %v", err)
	}

	// The first speaker starts the track from the top.
	must(t, r.SetSpeaker(host, true, at(time.Minute)))
	if p, _ := r.Position(at(time.Minute + 5*time.Second)); p != 5*time.Second {
		t.Fatalf("position after speaker joined = %v", p)
	}
	// Losing the last speaker pauses. Getting it back resumes.
	r.Disconnect(host, at(2*time.Minute))
	if v := r.View(host); !v.Now.Waiting {
		t.Fatal("room kept playing with no speaker")
	}
	must(t, r.Connect(host, at(3*time.Minute)))
	if p, _ := r.Position(at(3 * time.Minute)); p != time.Minute {
		t.Fatalf("position after reconnect = %v, want 1m", p)
	}
	// A manual pause survives a speaker reconnect.
	must(t, r.Pause(host, at(3*time.Minute)))
	r.Disconnect(host, at(4*time.Minute))
	must(t, r.Connect(host, at(5*time.Minute)))
	if v := r.View(host); v.Now.PausedAtMS == nil || v.Now.Waiting {
		t.Fatalf("manual pause lost: %+v", v.Now)
	}
}

func TestLeadPrefersHostThenFallsBack(t *testing.T) {
	r, host, g := party(t, 1)
	must(t, r.SetSpeaker(g[0], true, t0))
	if v := r.View(host); v.Lead != host {
		t.Fatalf("lead = %q, want host", v.Lead)
	}
	r.Disconnect(host, t0)
	if v := r.View(host); v.Lead != g[0] {
		t.Fatalf("lead = %q, want the guest speaker", v.Lead)
	}
	// The lead does not flap back when the host returns.
	must(t, r.Connect(host, t0))
	if v := r.View(host); v.Lead != g[0] {
		t.Fatalf("lead = %q, want it to stay with the guest", v.Lead)
	}
}

func TestLeadReportsDriveTheClock(t *testing.T) {
	r, host, g := party(t, 1)
	first, err := r.Add(host, Track{Provider: "youtube", ProviderID: "x", Title: "h1"}, t0)
	must(t, err)
	add(t, r, g[0], "a1")

	// Only the lead is heard.
	if changed, _ := r.Report(g[0], first, StateEnded, 0, 0, t0); changed {
		t.Fatal("a non-lead report changed the room")
	}
	// The lead fills in an unknown duration.
	changed, err := r.Report(host, first, StatePlaying, 1000, 200_000, at(time.Second))
	must(t, err)
	if !changed || r.View(host).Now.Item.Track.DurationMS != 200_000 {
		t.Fatal("duration was not learned from the lead")
	}
	// Small drift is left alone. Large drift moves the clock to the lead.
	if changed, _ := r.Report(host, first, StatePlaying, 9_500, 0, at(10*time.Second)); changed {
		t.Fatal("500ms of drift moved the clock")
	}
	if changed, _ := r.Report(host, first, StateBuffering, 6_000, 0, at(10*time.Second)); !changed {
		t.Fatal("4s of drift did not move the clock")
	}
	if p, _ := r.Position(at(10 * time.Second)); p != 6*time.Second {
		t.Fatalf("position = %v, want 6s", p)
	}
	// A report about an old item is ignored. The end of this one advances.
	if changed, _ := r.Report(host, "stale", StateEnded, 0, 0, t0); changed {
		t.Fatal("a stale report changed the room")
	}
	if changed, _ := r.Report(host, first, StateEnded, 0, 0, at(time.Minute)); !changed {
		t.Fatal("ended report did not advance")
	}
	if got := titles(r, host); got != "[a1]" {
		t.Fatalf("after end = %q", got)
	}
}

func TestTickAdvancesAStalledTrack(t *testing.T) {
	r, host, _ := party(t, 0)
	add(t, r, host, "h1")
	add(t, r, host, "h2")
	if r.Tick(at(185 * time.Second)) {
		t.Fatal("advanced inside the grace period")
	}
	if !r.Tick(at(189*time.Second)) || titles(r, host) != "[h2]" {
		t.Fatalf("did not advance a stalled track: %q", titles(r, host))
	}
}

func TestKickRemovesEverythingTheMemberOwned(t *testing.T) {
	r, host, g := party(t, 2)
	add(t, r, host, "h1")
	add(t, r, g[0], "a1")
	b1 := add(t, r, g[1], "b1")
	must(t, r.Vote(g[0], b1))
	must(t, r.Skip(g[0], t0))

	must(t, r.Kick(host, g[0], t0))
	v := r.View(host)
	if r.HasMember(g[0]) || len(v.Members) != 2 {
		t.Fatalf("member still present: %+v", v.Members)
	}
	if got := titles(r, host); got != "[h1] b1" {
		t.Fatalf("queue = %q", got)
	}
	if v.Queue[0].Votes != 0 || v.Now.SkipVotes != 0 {
		t.Fatalf("votes survived the kick: %+v %+v", v.Queue[0], v.Now)
	}
	if _, err := r.Add(g[0], track("x"), t0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("kicked member add err = %v", err)
	}
}

func TestLimitsAndEnd(t *testing.T) {
	r, host, g := party(t, 1)
	for i := 0; i <= MaxPendingPerMember; i++ {
		add(t, r, g[0], "t")
	}
	if _, err := r.Add(g[0], track("over"), t0); !errors.Is(err, ErrFull) {
		t.Fatalf("per-member cap err = %v", err)
	}
	if _, err := r.Add(host, Track{Provider: "youtube"}, t0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty track err = %v", err)
	}
	must(t, r.End(host))
	if _, err := r.Join("late"); !errors.Is(err, ErrEnded) {
		t.Fatalf("join after end err = %v", err)
	}
	if _, err := r.Add(host, track("x"), t0); !errors.Is(err, ErrEnded) {
		t.Fatalf("add after end err = %v", err)
	}
}

func TestCleanNickname(t *testing.T) {
	cases := map[string]string{
		"  Ada  ":                      "Ada",
		"":                             "Guest",
		"\x00\x1b[31m\n":               "[31m",
		"a \t\n b":                     "a b",
		strings.Repeat("x", 40):        strings.Repeat("x", MaxNickname),
		"<img src=x onerror=alert(1)>": "<img src=x onerror=alert",
	}
	for in, want := range cases {
		if got := CleanNickname(in); got != want {
			t.Errorf("CleanNickname(%q) = %q, want %q", in, got, want)
		}
	}
}
