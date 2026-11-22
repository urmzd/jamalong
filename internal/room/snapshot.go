package room

import "time"

// Snapshot is a summary of a room for logs. It holds no secrets.
type Snapshot struct {
	NowItem     string
	NowTitle    string
	NowProvider string
	NowBy       string
	PositionMS  int64
	DurationMS  int64
	Paused      bool
	Lead        string
	Queue       int
	Connected   int
	Speakers    int
}

// Snapshot summarizes the room at now.
func (r *Room) Snapshot(now time.Time) Snapshot {
	s := Snapshot{Lead: r.Nickname(r.lead), Queue: len(r.pending), Connected: r.Connected()}
	for _, m := range r.members {
		if m.activeSpeaker() {
			s.Speakers++
		}
	}
	if p := r.now; p != nil {
		s.NowItem = p.item.id
		s.NowTitle = p.item.track.Title
		s.NowProvider = p.item.track.Provider
		s.NowBy = r.Nickname(p.item.addedBy)
		s.PositionMS = p.position(now).Milliseconds()
		s.DurationMS = p.item.track.DurationMS
		s.Paused = p.paused
	}
	return s
}

// Nickname returns a member's nickname, or "" for an unknown ID.
func (r *Room) Nickname(id string) string {
	if m, ok := r.members[id]; ok {
		return m.nickname
	}
	return ""
}
