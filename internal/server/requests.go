package server

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/urmzd/jamalong/internal/room"
)

const (
	// requestTTL is how long a join request waits for the host.
	requestTTL = 10 * time.Minute
	// maxRequests bounds the requests waiting in one room.
	maxRequests = 20
	// maxListed bounds the open-room list.
	maxListed = 50
)

// joinRequest is a visitor asking the host to let them in. The visitor holds
// a token and polls with it. The server stores only the token's SHA-256.
type joinRequest struct {
	id       string // public, shown to the host
	nickname string
	at       time.Time
	member   string // set when the host admits the visitor
	denied   bool
}

func (q *joinRequest) waiting() bool { return q.member == "" && !q.denied }

// requestView is a waiting request as the host sees it.
type requestView struct {
	ID       string `json:"id"`
	Nickname string `json:"nickname"`
}

// openRoom is one row of the open-room list.
type openRoom struct {
	ID         string `json:"id"`
	Host       string `json:"host"`
	Listening  int    `json:"listening"`
	NowPlaying string `json:"now_playing,omitempty"`
}

// waitingRequests returns the requests the host still has to answer, oldest
// first. The caller holds sess.mu.
func (sess *session) waitingRequests() []requestView {
	out := []requestView{}
	for _, q := range sess.requests {
		if q.waiting() {
			out = append(out, requestView{ID: q.id, Nickname: q.nickname})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// pruneRequests drops requests older than requestTTL and reports whether a
// waiting one was dropped. The caller holds sess.mu.
func (sess *session) pruneRequests(now time.Time) bool {
	dropped := false
	for h, q := range sess.requests {
		if now.Sub(q.at) > requestTTL {
			dropped = dropped || q.waiting()
			delete(sess.requests, h)
		}
	}
	return dropped
}

// findRequest returns the request with a public ID. The caller holds sess.mu.
func (sess *session) findRequest(id string) *joinRequest {
	for _, q := range sess.requests {
		if q.id == id {
			return q
		}
	}
	return nil
}

// handleList returns the rooms that have at least one person connected.
// Joining one still needs the link or the host's approval.
func (s *Server) handleList(w http.ResponseWriter, _ *http.Request) {
	now := s.cfg.Now()
	s.mu.Lock()
	all := make([]*session, 0, len(s.rooms))
	for _, sess := range s.rooms {
		all = append(all, sess)
	}
	s.mu.Unlock()

	rooms := []openRoom{}
	for _, sess := range all {
		sess.mu.Lock()
		if !sess.room.Ended() && sess.room.Connected() > 0 {
			snap := sess.room.Snapshot(now)
			rooms = append(rooms, openRoom{
				ID:         sess.room.ID(),
				Host:       sess.room.Nickname(sess.host),
				Listening:  snap.Connected,
				NowPlaying: snap.NowTitle,
			})
		}
		sess.mu.Unlock()
	}
	sort.Slice(rooms, func(i, j int) bool {
		if rooms[i].Listening != rooms[j].Listening {
			return rooms[i].Listening > rooms[j].Listening
		}
		return rooms[i].ID < rooms[j].ID
	})
	if len(rooms) > maxListed {
		rooms = rooms[:maxListed]
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"rooms": rooms})
}

// handleRequest records a visitor's request to join and tells the host.
func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	var body nicknameBody
	if !s.accept(w, r, s.joins, &body) {
		return
	}
	sess := s.lookup(r.PathValue("id"))
	if sess == nil {
		writeError(w, http.StatusNotFound, "not_found", "this room has ended")
		return
	}
	now := s.cfg.Now()
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.room.Ended() {
		writeError(w, http.StatusNotFound, "not_found", "this room has ended")
		return
	}
	sess.pruneRequests(now)
	if len(sess.waitingRequests()) >= maxRequests {
		writeError(w, http.StatusConflict, "full", "too many people are waiting, try again later")
		return
	}
	nickname := room.CleanNickname(body.Nickname)
	token := randomToken(32)
	q := &joinRequest{id: room.NewID(), nickname: nickname, at: now}
	sess.requests[sha256.Sum256([]byte(token))] = q
	s.cfg.Logger.Info("join requested", "room", sess.room.ID(), "nickname", nickname)
	sess.broadcast()
	writeJSON(w, http.StatusCreated, map[string]string{"request": token})
}

type claimBody struct {
	Request string `json:"request"`
}

// handleClaim tells a waiting visitor whether the host answered. Once
// admitted, it sets the session cookie, the same as joining with the link.
func (s *Server) handleClaim(w http.ResponseWriter, r *http.Request) {
	var body claimBody
	if !s.accept(w, r, s.polls, &body) {
		return
	}
	const gone = "this request has expired, ask again"
	sess := s.lookup(r.PathValue("id"))
	if sess == nil {
		writeError(w, http.StatusNotFound, "not_found", gone)
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	key := sha256.Sum256([]byte(body.Request))
	q, ok := sess.requests[key]
	if !ok || sess.room.Ended() || s.cfg.Now().Sub(q.at) > requestTTL {
		writeError(w, http.StatusNotFound, "not_found", gone)
		return
	}
	switch {
	case q.denied:
		delete(sess.requests, key)
		writeError(w, http.StatusForbidden, "denied", "the host did not let you in")
	case q.member != "":
		delete(sess.requests, key)
		s.issue(w, sess, q.member)
		writeJSON(w, http.StatusOK, map[string]string{"status": "admitted", roomIDKey: sess.room.ID()})
	default:
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "waiting"})
	}
}

// answer lets the host admit or deny a request. The caller holds sess.mu.
func (s *Server) answer(sess *session, by, requestID string, admit bool) error {
	if by != sess.host {
		return fmt.Errorf("host only: %w", room.ErrForbidden)
	}
	q := sess.findRequest(requestID)
	if q == nil || !q.waiting() {
		return fmt.Errorf("request: %w", room.ErrNotFound)
	}
	if !admit {
		q.denied = true
		s.cfg.Logger.Info("join denied", "room", sess.room.ID(), "nickname", q.nickname)
		sess.broadcast()
		return nil
	}
	member, err := sess.room.Join(q.nickname)
	if err != nil {
		return err
	}
	q.member = member
	s.cfg.Logger.Info("join admitted", "room", sess.room.ID(), "nickname", q.nickname)
	sess.broadcast()
	return nil
}
