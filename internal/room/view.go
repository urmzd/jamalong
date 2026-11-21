package room

import "time"

// View is the room as one member sees it. It is the only shape sent to
// clients.
type View struct {
	ID      string       `json:"id"`
	Version uint64       `json:"version"`
	You     string       `json:"you"`
	Lead    string       `json:"lead,omitempty"`
	Members []MemberView `json:"members"`
	Queue   []ItemView   `json:"queue"`
	Now     *NowView     `json:"now"`
}

// MemberView is one member of the room.
type MemberView struct {
	ID        string `json:"id"`
	Nickname  string `json:"nickname"`
	Role      Role   `json:"role"`
	Speaker   bool   `json:"speaker"`
	Connected bool   `json:"connected"`
}

// ItemView is one queued track.
type ItemView struct {
	ID      string `json:"id"`
	Track   Track  `json:"track"`
	AddedBy string `json:"added_by"`
	Votes   int    `json:"votes"`
	Voted   bool   `json:"voted"`
}

// NowView is the track that is playing and the clock that places it. A
// speaker's position is the server time minus StartedAtMS, or PausedAtMS
// minus StartedAtMS while paused.
type NowView struct {
	Item        ItemView `json:"item"`
	StartedAtMS int64    `json:"started_at_ms"`
	PausedAtMS  *int64   `json:"paused_at_ms"`
	// Waiting is true when the room paused itself because no speaker is
	// connected.
	Waiting bool `json:"waiting"`
	// SeekSeq grows on every explicit seek. A lead speaker jumps to the clock
	// when it changes, and at no other time.
	SeekSeq    uint64 `json:"seek_seq"`
	SkipVotes  int    `json:"skip_votes"`
	SkipNeeded int    `json:"skip_needed"`
	SkipVoted  bool   `json:"skip_voted"`
}

func (it *item) view(viewer string) ItemView {
	_, voted := it.votes[viewer]
	return ItemView{ID: it.id, Track: it.track, AddedBy: it.addedBy, Votes: len(it.votes), Voted: voted}
}

// View returns the room as the given member sees it.
func (r *Room) View(viewer string) View {
	v := View{
		ID:      r.id,
		Version: r.version,
		You:     viewer,
		Lead:    r.lead,
		Members: make([]MemberView, 0, len(r.joined)),
		Queue:   make([]ItemView, 0, len(r.pending)),
	}
	for _, id := range r.joined {
		m := r.members[id]
		v.Members = append(v.Members, MemberView{
			ID: m.id, Nickname: m.nickname, Role: m.role, Speaker: m.speaker, Connected: m.conns > 0,
		})
	}
	for _, it := range r.ordered() {
		v.Queue = append(v.Queue, it.view(viewer))
	}
	if p := r.now; p != nil {
		_, voted := p.skipVotes[viewer]
		n := &NowView{
			Item:        p.item.view(viewer),
			StartedAtMS: p.startedAt.UnixMilli(),
			Waiting:     p.paused && p.autoPaused,
			SeekSeq:     p.seekSeq,
			SkipVotes:   len(p.skipVotes),
			SkipNeeded:  r.skipNeeded(),
			SkipVoted:   voted,
		}
		if p.paused {
			ms := p.pausedAt.UnixMilli()
			n.PausedAtMS = &ms
		}
		v.Now = n
	}
	return v
}

// Position returns how far into the current track the room clock is, and
// false when nothing is playing.
func (r *Room) Position(now time.Time) (time.Duration, bool) {
	if r.now == nil {
		return 0, false
	}
	return r.now.position(now), true
}
