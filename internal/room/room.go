// Package room holds the state of one listening room: its members, its queue
// and its playback clock.
//
// A Room does no I/O and never reads the wall clock. Callers pass the current
// time to every method that needs it, and must serialize access themselves.
package room

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Limits that bound the size of a room.
const (
	MaxMembers          = 100
	MaxQueue            = 200
	MaxPendingPerMember = 25
	MaxNickname         = 24
	maxTrackField       = 300
	maxDuration         = 24 * time.Hour

	// driftTolerance is how far the lead speaker may drift from the room
	// clock before the clock follows it.
	driftTolerance = 1500 * time.Millisecond
	// stallGrace is how long past a track's known duration the room waits for
	// the lead speaker to report the end before it moves on.
	stallGrace = 8 * time.Second
)

// Errors returned by Room methods. Wrap-checked with errors.Is.
var (
	ErrForbidden = errors.New("forbidden")
	ErrNotFound  = errors.New("not found")
	ErrInvalid   = errors.New("invalid")
	ErrFull      = errors.New("full")
	ErrEnded     = errors.New("room ended")
)

// Role is what a member may do in a room.
type Role string

// The three roles. A room has exactly one host.
const (
	RoleHost         Role = "host"
	RoleCollaborator Role = "collaborator"
	RoleGuest        Role = "guest"
)

// PlayerState is what a speaker reports about its own player.
type PlayerState string

// States a speaker can report.
const (
	StatePlaying   PlayerState = "playing"
	StateBuffering PlayerState = "buffering"
	StateEnded     PlayerState = "ended"
)

// Track identifies one playable recording on one provider.
type Track struct {
	Provider   string `json:"provider"`
	ProviderID string `json:"provider_id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	DurationMS int64  `json:"duration_ms"`
	ISRC       string `json:"isrc,omitempty"`
	ArtworkURL string `json:"artwork_url,omitempty"`
}

type member struct {
	id       string
	nickname string
	role     Role
	speaker  bool
	conns    int
}

func (m *member) activeSpeaker() bool { return m.speaker && m.conns > 0 }

type item struct {
	id      string
	track   Track
	addedBy string
	round   int
	seq     int64
	votes   map[string]struct{}
}

type playing struct {
	item       *item
	startedAt  time.Time
	paused     bool
	pausedAt   time.Time
	autoPaused bool
	skipVotes  map[string]struct{}
}

func (p *playing) position(now time.Time) time.Duration {
	if p.paused {
		return p.pausedAt.Sub(p.startedAt)
	}
	return now.Sub(p.startedAt)
}

// Room is one listening room. It is not safe for concurrent use.
type Room struct {
	id      string
	version uint64
	members map[string]*member
	joined  []string
	pending []*item
	now     *playing
	// round is the fairness round of the item that last started playing.
	round int
	// served maps a member to the last round in which one of their items
	// started playing.
	served map[string]int
	seq    int64
	lead   string
	ended  bool
}

// NewID returns a random 64-bit identifier in hex. IDs are public handles,
// not secrets.
func NewID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("room: crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b[:])
}

// New creates a room and its host, and returns the host's member ID.
func New(id, hostNickname string) (*Room, string) {
	r := &Room{id: id, members: map[string]*member{}, served: map[string]int{}}
	return r, r.addMember(hostNickname, RoleHost)
}

func (r *Room) addMember(nickname string, role Role) string {
	m := &member{id: NewID(), nickname: CleanNickname(nickname), role: role}
	r.members[m.id] = m
	r.joined = append(r.joined, m.id)
	r.version++
	return m.id
}

// CleanNickname makes a nickname safe to store and show: printable characters
// only, single spaces, at most MaxNickname runes, never empty.
func CleanNickname(s string) string {
	var b strings.Builder
	n := 0
	space := false
	for _, c := range strings.TrimSpace(s) {
		if unicode.IsSpace(c) {
			space = true
			continue
		}
		if !unicode.IsPrint(c) {
			continue
		}
		if space && n > 0 {
			if n+1 >= MaxNickname {
				break
			}
			b.WriteRune(' ')
			n++
		}
		space = false
		if n >= MaxNickname {
			break
		}
		b.WriteRune(c)
		n++
	}
	if n == 0 {
		return "Guest"
	}
	return b.String()
}

// ID returns the room's public identifier.
func (r *Room) ID() string { return r.id }

// Version returns a counter that grows with every visible change.
func (r *Room) Version() uint64 { return r.version }

// Ended reports whether the host has ended the room.
func (r *Room) Ended() bool { return r.ended }

// HasMember reports whether id is a current member.
func (r *Room) HasMember(id string) bool { _, ok := r.members[id]; return ok }

// Connected returns how many members have at least one live connection.
func (r *Room) Connected() int {
	n := 0
	for _, m := range r.members {
		if m.conns > 0 {
			n++
		}
	}
	return n
}

func (r *Room) actor(id string) (*member, error) {
	if r.ended {
		return nil, ErrEnded
	}
	m, ok := r.members[id]
	if !ok {
		return nil, fmt.Errorf("member: %w", ErrNotFound)
	}
	return m, nil
}

func (m *member) controls() bool { return m.role == RoleHost || m.role == RoleCollaborator }

// Join adds a guest and returns their member ID.
func (r *Room) Join(nickname string) (string, error) {
	if r.ended {
		return "", ErrEnded
	}
	if len(r.members) >= MaxMembers {
		return "", fmt.Errorf("members: %w", ErrFull)
	}
	return r.addMember(nickname, RoleGuest), nil
}

// Connect records one more live connection for a member.
func (r *Room) Connect(id string, now time.Time) error {
	m, err := r.actor(id)
	if err != nil {
		return err
	}
	m.conns++
	r.refreshSpeakers(now)
	r.version++
	return nil
}

// Disconnect records one fewer live connection for a member.
func (r *Room) Disconnect(id string, now time.Time) {
	m, ok := r.members[id]
	if !ok || m.conns == 0 {
		return
	}
	m.conns--
	if m.conns == 0 && r.now != nil {
		delete(r.now.skipVotes, id)
	}
	r.refreshSpeakers(now)
	r.version++
}

// SetSpeaker turns a member's device into a speaker, or back into a remote.
func (r *Room) SetSpeaker(by string, on bool, now time.Time) error {
	m, err := r.actor(by)
	if err != nil {
		return err
	}
	if m.speaker == on {
		return nil
	}
	m.speaker = on
	r.refreshSpeakers(now)
	r.version++
	return nil
}

// refreshSpeakers re-picks the lead speaker, pauses when the last speaker
// leaves and resumes when one comes back.
func (r *Room) refreshSpeakers(now time.Time) {
	r.lead = r.pickLead()
	p := r.now
	if p == nil {
		return
	}
	switch {
	case r.lead == "" && !p.paused:
		p.paused, p.pausedAt, p.autoPaused = true, now, true
	case r.lead != "" && p.paused && p.autoPaused:
		p.startedAt = p.startedAt.Add(now.Sub(p.pausedAt))
		p.paused, p.autoPaused = false, false
	}
}

// pickLead keeps the current lead while it is still playing, then prefers the
// host, then the longest-standing member.
func (r *Room) pickLead() string {
	if m, ok := r.members[r.lead]; ok && m.activeSpeaker() {
		return r.lead
	}
	first := ""
	for _, id := range r.joined {
		m := r.members[id]
		if !m.activeSpeaker() {
			continue
		}
		if m.role == RoleHost {
			return id
		}
		if first == "" {
			first = id
		}
	}
	return first
}

func validTrack(t Track) error {
	switch {
	case t.Provider == "" || t.ProviderID == "" || strings.TrimSpace(t.Title) == "":
		return fmt.Errorf("track needs a provider, an id and a title: %w", ErrInvalid)
	case len(t.Provider) > maxTrackField || len(t.ProviderID) > maxTrackField ||
		len(t.Title) > maxTrackField || len(t.Artist) > maxTrackField ||
		len(t.ISRC) > maxTrackField || len(t.ArtworkURL) > 2*maxTrackField:
		return fmt.Errorf("track field too long: %w", ErrInvalid)
	case t.DurationMS < 0 || t.DurationMS > maxDuration.Milliseconds():
		return fmt.Errorf("track duration out of range: %w", ErrInvalid)
	}
	return nil
}

// Add puts a track in the queue and returns the new item's ID. If nothing is
// playing, the track starts.
//
// Order is fair by person. Each item gets a round: a member's next item goes
// one round after their last, and never before the round now playing. So a
// member who queues ten tracks cannot push everyone else back ten places.
func (r *Room) Add(by string, t Track, now time.Time) (string, error) {
	if _, err := r.actor(by); err != nil {
		return "", err
	}
	if err := validTrack(t); err != nil {
		return "", err
	}
	if len(r.pending) >= MaxQueue {
		return "", fmt.Errorf("queue: %w", ErrFull)
	}
	round, mine := r.round, 0
	if last, ok := r.served[by]; ok && last+1 > round {
		round = last + 1
	}
	for _, it := range r.pending {
		if it.addedBy != by {
			continue
		}
		mine++
		if it.round+1 > round {
			round = it.round + 1
		}
	}
	if mine >= MaxPendingPerMember {
		return "", fmt.Errorf("your queued tracks: %w", ErrFull)
	}
	r.seq++
	it := &item{id: NewID(), track: t, addedBy: by, round: round, seq: r.seq, votes: map[string]struct{}{}}
	r.pending = append(r.pending, it)
	if r.now == nil {
		r.advance(now)
	}
	r.version++
	return it.id, nil
}

// ordered returns the pending items in play order: by round, then by votes,
// then by arrival.
func (r *Room) ordered() []*item {
	out := append([]*item(nil), r.pending...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.round != b.round {
			return a.round < b.round
		}
		if len(a.votes) != len(b.votes) {
			return len(a.votes) > len(b.votes)
		}
		return a.seq < b.seq
	})
	return out
}

// advance starts the next pending item, or leaves the room idle.
func (r *Room) advance(now time.Time) {
	next := r.ordered()
	if len(next) == 0 {
		r.now = nil
		return
	}
	it := next[0]
	r.dropPending(it.id)
	r.round = it.round
	r.served[it.addedBy] = it.round
	p := &playing{item: it, startedAt: now, skipVotes: map[string]struct{}{}}
	if r.lead == "" {
		p.paused, p.pausedAt, p.autoPaused = true, now, true
	}
	r.now = p
}

func (r *Room) dropPending(id string) bool {
	for i, it := range r.pending {
		if it.id == id {
			r.pending = append(r.pending[:i], r.pending[i+1:]...)
			return true
		}
	}
	return false
}

func (r *Room) findPending(id string) *item {
	for _, it := range r.pending {
		if it.id == id {
			return it
		}
	}
	return nil
}

// Remove takes a pending item out of the queue. Members may remove their own
// items. The host and collaborators may remove anyone's.
func (r *Room) Remove(by, itemID string) error {
	m, err := r.actor(by)
	if err != nil {
		return err
	}
	it := r.findPending(itemID)
	if it == nil {
		return fmt.Errorf("item: %w", ErrNotFound)
	}
	if it.addedBy != by && !m.controls() {
		return fmt.Errorf("remove another member's track: %w", ErrForbidden)
	}
	r.dropPending(itemID)
	r.version++
	return nil
}

// Vote toggles a member's vote on a pending item. Votes order items within a
// round. Members cannot vote for their own items.
func (r *Room) Vote(by, itemID string) error {
	if _, err := r.actor(by); err != nil {
		return err
	}
	it := r.findPending(itemID)
	if it == nil {
		return fmt.Errorf("item: %w", ErrNotFound)
	}
	if it.addedBy == by {
		return fmt.Errorf("vote for your own track: %w", ErrInvalid)
	}
	if _, ok := it.votes[by]; ok {
		delete(it.votes, by)
	} else {
		it.votes[by] = struct{}{}
	}
	r.version++
	return nil
}

// skipNeeded is the number of guest votes that skip the current track: a
// strict majority of connected members.
func (r *Room) skipNeeded() int { return r.Connected()/2 + 1 }

// Skip moves to the next track. The host and collaborators skip at once. A
// guest's call toggles their vote, and the track skips on a majority.
func (r *Room) Skip(by string, now time.Time) error {
	m, err := r.actor(by)
	if err != nil {
		return err
	}
	p := r.now
	if p == nil {
		return fmt.Errorf("nothing is playing: %w", ErrInvalid)
	}
	if m.controls() {
		r.advance(now)
		r.version++
		return nil
	}
	if _, ok := p.skipVotes[by]; ok {
		delete(p.skipVotes, by)
	} else {
		p.skipVotes[by] = struct{}{}
	}
	if len(p.skipVotes) >= r.skipNeeded() {
		r.advance(now)
	}
	r.version++
	return nil
}

func (r *Room) control(by string) (*playing, error) {
	m, err := r.actor(by)
	if err != nil {
		return nil, err
	}
	if !m.controls() {
		return nil, fmt.Errorf("playback control: %w", ErrForbidden)
	}
	if r.now == nil {
		return nil, fmt.Errorf("nothing is playing: %w", ErrInvalid)
	}
	return r.now, nil
}

// Pause stops the clock. Host and collaborators only.
func (r *Room) Pause(by string, now time.Time) error {
	p, err := r.control(by)
	if err != nil {
		return err
	}
	if p.paused && !p.autoPaused {
		return nil
	}
	if !p.paused {
		p.pausedAt = now
	}
	p.paused, p.autoPaused = true, false
	r.version++
	return nil
}

// Play restarts the clock. Host and collaborators only. It needs a speaker.
func (r *Room) Play(by string, now time.Time) error {
	p, err := r.control(by)
	if err != nil {
		return err
	}
	if !p.paused {
		return nil
	}
	if r.lead == "" {
		return fmt.Errorf("no speaker is connected: %w", ErrInvalid)
	}
	p.startedAt = p.startedAt.Add(now.Sub(p.pausedAt))
	p.paused, p.autoPaused = false, false
	r.version++
	return nil
}

// Seek moves the clock to a position in the current track. Host and
// collaborators only.
func (r *Room) Seek(by string, positionMS int64, now time.Time) error {
	p, err := r.control(by)
	if err != nil {
		return err
	}
	if positionMS < 0 {
		positionMS = 0
	}
	if d := p.item.track.DurationMS; d > 0 && positionMS > d {
		positionMS = d
	}
	ref := now
	if p.paused {
		ref = p.pausedAt
	}
	p.startedAt = ref.Add(-time.Duration(positionMS) * time.Millisecond)
	r.version++
	return nil
}

// SetRole makes a member a collaborator or a guest. Host only.
func (r *Room) SetRole(by, target string, role Role) error {
	t, err := r.hostTarget(by, target)
	if err != nil {
		return err
	}
	if role != RoleCollaborator && role != RoleGuest {
		return fmt.Errorf("role %q: %w", role, ErrInvalid)
	}
	t.role = role
	r.version++
	return nil
}

func (r *Room) hostTarget(by, target string) (*member, error) {
	m, err := r.actor(by)
	if err != nil {
		return nil, err
	}
	if m.role != RoleHost {
		return nil, fmt.Errorf("host only: %w", ErrForbidden)
	}
	if by == target {
		return nil, fmt.Errorf("cannot target yourself: %w", ErrInvalid)
	}
	t, ok := r.members[target]
	if !ok {
		return nil, fmt.Errorf("member: %w", ErrNotFound)
	}
	return t, nil
}

// Kick removes a member, their pending items and their votes. Host only.
func (r *Room) Kick(by, target string, now time.Time) error {
	if _, err := r.hostTarget(by, target); err != nil {
		return err
	}
	delete(r.members, target)
	delete(r.served, target)
	for i, id := range r.joined {
		if id == target {
			r.joined = append(r.joined[:i], r.joined[i+1:]...)
			break
		}
	}
	kept := r.pending[:0]
	for _, it := range r.pending {
		if it.addedBy != target {
			delete(it.votes, target)
			kept = append(kept, it)
		}
	}
	r.pending = kept
	if r.now != nil {
		delete(r.now.skipVotes, target)
	}
	r.refreshSpeakers(now)
	r.version++
	return nil
}

// End closes the room for good. Host only.
func (r *Room) End(by string) error {
	m, err := r.actor(by)
	if err != nil {
		return err
	}
	if m.role != RoleHost {
		return fmt.Errorf("host only: %w", ErrForbidden)
	}
	r.ended = true
	r.version++
	return nil
}

// Report takes the lead speaker's view of its player. An ended track moves
// the queue on. A position far from the room clock moves the clock, so the
// room follows the device that is really making sound. Reports from anyone
// else, or about a track that is no longer current, are ignored. It returns
// whether the room changed.
func (r *Room) Report(by, itemID string, state PlayerState, positionMS, durationMS int64, now time.Time) (bool, error) {
	if _, err := r.actor(by); err != nil {
		return false, err
	}
	p := r.now
	if p == nil || by != r.lead || p.item.id != itemID {
		return false, nil
	}
	changed := false
	switch state {
	case StateEnded:
		r.advance(now)
		changed = true
	case StatePlaying, StateBuffering:
		if p.item.track.DurationMS == 0 && durationMS > 0 && durationMS <= maxDuration.Milliseconds() {
			p.item.track.DurationMS = durationMS
			changed = true
		}
		if !p.paused && positionMS >= 0 {
			reported := time.Duration(positionMS) * time.Millisecond
			if drift := p.position(now) - reported; drift > driftTolerance || drift < -driftTolerance {
				p.startedAt = now.Add(-reported)
				changed = true
			}
		}
	default:
		return false, fmt.Errorf("player state %q: %w", state, ErrInvalid)
	}
	if changed {
		r.version++
	}
	return changed, nil
}

// Tick moves the queue on when a track has run well past its known duration
// and the lead speaker never reported the end. It returns whether the room
// changed.
func (r *Room) Tick(now time.Time) bool {
	p := r.now
	if r.ended || p == nil || p.paused || p.item.track.DurationMS == 0 {
		return false
	}
	limit := time.Duration(p.item.track.DurationMS)*time.Millisecond + stallGrace
	if p.position(now) <= limit {
		return false
	}
	r.advance(now)
	r.version++
	return true
}
