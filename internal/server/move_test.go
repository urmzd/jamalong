package server

import (
	"strings"
	"testing"
)

func TestMoveOverTheSocket(t *testing.T) {
	base, host, roomID, secret := start(t)
	guest := join(t, base, roomID, secret, "Ada")
	host.send(map[string]any{"type": "speaker", "on": true})
	for _, title := range []string{"a", "b", "c", "d"} {
		host.send(map[string]any{"type": "add_url", "url": "ok:" + title})
	}
	order := func(f frame) string {
		out := []string{}
		for _, it := range f.Room.Queue {
			out = append(out, it.Track.Title)
		}
		return strings.Join(out, " ")
	}
	f := host.until("queue filled", func(f frame) bool { return isState(f) && len(f.Room.Queue) == 3 })
	d := f.Room.Queue[2].ID

	host.send(map[string]any{"type": "move", "item_id": d, "index": 0})
	guest.until("guest sees d next", func(f frame) bool { return isState(f) && order(f) == "d b c" })

	guest.send(map[string]any{"type": "move", "item_id": d, "index": 2})
	if g := guest.until("forbidden", func(f frame) bool { return f.Type == "error" }); g.Code != "forbidden" || g.Ref != "move" {
		t.Fatalf("guest move = %+v", g)
	}
}
