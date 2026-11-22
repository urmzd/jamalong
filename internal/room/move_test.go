package room

import (
	"errors"
	"testing"
)

func TestMoveAndPlayNext(t *testing.T) {
	r, host, guests := party(t, 1)
	g := guests[0]
	add(t, r, host, "a") // starts playing
	b := add(t, r, host, "b")
	add(t, r, g, "c")
	d := add(t, r, host, "d")
	if got := titles(r, host); got != "[a] c b d" {
		t.Fatalf("fair order = %q", got)
	}

	// Play next: d jumps to the front.
	must(t, r.Move(host, d, 0))
	if got := titles(r, host); got != "[a] d c b" {
		t.Fatalf("after play next = %q", got)
	}
	// Move down one.
	must(t, r.Move(host, d, 1))
	if got := titles(r, host); got != "[a] c d b" {
		t.Fatalf("after move down = %q", got)
	}
	// An index past the end clamps to the end.
	must(t, r.Move(host, c(t, r, host), 99))
	if got := titles(r, host); got != "[a] d b c" {
		t.Fatalf("after move to end = %q", got)
	}

	// A track added later goes after the hand-set order, and votes do not
	// lift it over it.
	e := add(t, r, g, "e")
	must(t, r.Vote(host, e))
	if got := titles(r, host); got != "[a] d b c e" {
		t.Fatalf("after add = %q", got)
	}

	// The queue plays in the shown order, and removal keeps the rest.
	must(t, r.Remove(host, b))
	must(t, r.Skip(host, t0))
	if got := titles(r, host); got != "[d] c e" {
		t.Fatalf("after skip = %q", got)
	}

	// Guests cannot reorder; unknown items are refused.
	if err := r.Move(g, e, 0); !errors.Is(err, ErrForbidden) {
		t.Errorf("guest move err = %v, want ErrForbidden", err)
	}
	if err := r.Move(host, "nope", 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown item err = %v, want ErrNotFound", err)
	}
}

// c returns the ID of the pending item titled "c".
func c(t *testing.T, r *Room, viewer string) string {
	t.Helper()
	for _, it := range r.View(viewer).Queue {
		if it.Track.Title == "c" {
			return it.ID
		}
	}
	t.Fatal("no item c")
	return ""
}
