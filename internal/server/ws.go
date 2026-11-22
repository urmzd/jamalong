package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/urmzd/jamalong/internal/room"
)

const (
	readLimit    = 4 << 10
	sendBuffer   = 32
	writeTimeout = 10 * time.Second
	// maxConnsPerMember bounds how many tabs one session may hold open.
	maxConnsPerMember = 4
)

// conn is one WebSocket. Its fields are guarded by the session mutex.
type conn struct {
	member string
	send   chan []byte
	closed bool
	// reason is sent as the close reason when the server ends the connection.
	reason string
}

// inbound is every command a client can send. Type selects which fields are
// read.
type inbound struct {
	Type       string           `json:"type"`
	URL        string           `json:"url"`
	ItemID     string           `json:"item_id"`
	RequestID  string           `json:"request_id"`
	MemberID   string           `json:"member_id"`
	Role       room.Role        `json:"role"`
	On         bool             `json:"on"`
	State      room.PlayerState `json:"state"`
	PositionMS int64            `json:"position_ms"`
	DurationMS int64            `json:"duration_ms"`
	T0         float64          `json:"t0"`
	// Reason says why a speaker reported a track ended. It is only logged.
	Reason string `json:"reason"`
	// Event and Provider describe a speaker's player event. Only logged.
	Event    string `json:"event"`
	Provider string `json:"provider"`
}

type stateMsg struct {
	Type string    `json:"type"`
	Room room.View `json:"room"`
	// JoinSecret is sent to the host only, so the host can show the link.
	JoinSecret string `json:"join_secret,omitempty"`
	// Requests are the visitors waiting for the host. Host only.
	Requests []requestView `json:"requests,omitempty"`
	ServerMS int64         `json:"server_ms"`
}

type errorMsg struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
	// Ref is the type of the command that failed.
	Ref string `json:"ref,omitempty"`
}

func failure(code, message, ref string) errorMsg {
	return errorMsg{Type: "error", Code: code, Message: message, Ref: ref}
}

type pongMsg struct {
	Type     string  `json:"type"`
	T0       float64 `json:"t0"`
	ServerMS int64   `json:"server_ms"`
}

// enqueue sends to one connection without blocking. A connection that cannot
// keep up is dropped. The caller holds sess.mu.
func (sess *session) enqueue(c *conn, v any) {
	if c.closed {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	select {
	case c.send <- b:
	default:
		sess.drop(c, "slow")
	}
}

// drop closes a connection's outbound queue. The writer then closes the
// socket with the reason. The caller holds sess.mu.
func (sess *session) drop(c *conn, reason string) {
	if c.closed {
		return
	}
	c.closed, c.reason = true, reason
	close(c.send)
}

// broadcast sends every connection its member's view. The caller holds
// sess.mu.
func (sess *session) broadcast() {
	now := sess.clock().UnixMilli()
	for c := range sess.conns {
		msg := stateMsg{Type: "state", Room: sess.room.View(c.member), ServerMS: now}
		if c.member == sess.host {
			msg.JoinSecret = sess.secret
			msg.Requests = sess.waitingRequests()
		}
		sess.enqueue(c, msg)
	}
}

// evict drops every connection and session token of one member. The caller
// holds sess.mu.
func (sess *session) evict(member, reason string) {
	for c := range sess.conns {
		if c.member == member {
			sess.drop(c, reason)
		}
	}
	for h, m := range sess.tokens {
		if m == member {
			delete(sess.tokens, h)
		}
	}
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if !s.upgrades.allow(clientIP(r), s.cfg.Now()) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many connections, wait a minute")
		return
	}
	sess, member, ok := s.authed(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "join the room first")
		return
	}
	// Accept rejects cross-origin upgrades, which is what stops another site
	// from riding the session cookie.
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(readLimit)
	c := &conn{member: member, send: make(chan []byte, sendBuffer)}

	sess.mu.Lock()
	open := 0
	for other := range sess.conns {
		if other.member == member {
			open++
		}
	}
	if open >= maxConnsPerMember {
		sess.mu.Unlock()
		_ = ws.Close(websocket.StatusPolicyViolation, "too_many_tabs")
		return
	}
	connectBefore := sess.room.Snapshot(s.cfg.Now())
	if err := sess.room.Connect(member, s.cfg.Now()); err != nil {
		sess.mu.Unlock()
		_ = ws.Close(websocket.StatusPolicyViolation, "gone")
		return
	}
	sess.conns[c] = struct{}{}
	logChanges(s.cfg.Logger, sess.room.ID(), connectBefore, sess.room.Snapshot(s.cfg.Now()),
		"connected: "+sess.room.Nickname(member))
	sess.broadcast()
	sess.mu.Unlock()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		defer cancel()
		for msg := range c.send {
			wctx, done := context.WithTimeout(ctx, writeTimeout)
			err := ws.Write(wctx, websocket.MessageText, msg)
			done()
			if err != nil {
				return
			}
		}
		_ = ws.Close(websocket.StatusNormalClosure, c.reason)
	}()

	limit := &tokens{level: 20, burst: 20, rate: 5}
	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			break
		}
		var in inbound
		if json.Unmarshal(data, &in) != nil {
			s.reply(sess, c, failure("invalid", "malformed message", ""))
			continue
		}
		if !limit.take(s.cfg.Now()) {
			s.reply(sess, c, failure("rate_limited", "slow down", in.Type))
			continue
		}
		if err := s.dispatch(ctx, sess, c, in); err != nil {
			code := errorCode(err)
			msg := err.Error()
			if code == "internal" {
				s.cfg.Logger.Warn("command failed", "type", in.Type, "err", err)
				msg = "something went wrong"
			}
			s.reply(sess, c, failure(code, msg, in.Type))
		}
	}

	sess.mu.Lock()
	delete(sess.conns, c)
	sess.drop(c, "")
	disconnectBefore := sess.room.Snapshot(s.cfg.Now())
	sess.room.Disconnect(member, s.cfg.Now())
	logChanges(s.cfg.Logger, sess.room.ID(), disconnectBefore, sess.room.Snapshot(s.cfg.Now()),
		"disconnected: "+sess.room.Nickname(member))
	sess.broadcast()
	sess.mu.Unlock()
	_ = ws.CloseNow()
}

func (s *Server) reply(sess *session, c *conn, v any) {
	sess.mu.Lock()
	sess.enqueue(c, v)
	sess.mu.Unlock()
}

// dispatch applies one command and broadcasts if the room changed.
func (s *Server) dispatch(ctx context.Context, sess *session, c *conn, in inbound) error {
	now := s.cfg.Now()
	by := c.member

	switch in.Type {
	case "ping":
		s.reply(sess, c, pongMsg{Type: "pong", T0: in.T0, ServerMS: now.UnixMilli()})
		return nil
	case "player":
		sess.mu.Lock()
		who := sess.room.Nickname(by)
		snap := sess.room.Snapshot(now)
		sess.mu.Unlock()
		s.cfg.Logger.Info("player", "room", sess.room.ID(), "member", who,
			"event", reasonText(in.Event), "provider", reasonText(in.Provider),
			"current", in.ItemID == snap.NowItem, "player_at_ms", in.PositionMS,
			"player_duration_ms", in.DurationMS, "clock_ms", snap.PositionMS)
		return nil
	case "add_url":
		if s.cfg.Resolver == nil {
			return fmt.Errorf("no provider is configured: %w", room.ErrInvalid)
		}
		// Resolving talks to the provider, so it runs outside the room lock.
		rctx, cancel := context.WithTimeout(ctx, resolveBudget)
		track, err := s.cfg.Resolver.Resolve(rctx, in.URL)
		cancel()
		if err != nil {
			return err
		}
		sess.mu.Lock()
		defer sess.mu.Unlock()
		before := sess.room.Snapshot(s.cfg.Now())
		if _, err := sess.room.Add(by, track, s.cfg.Now()); err != nil {
			return err
		}
		after := sess.room.Snapshot(s.cfg.Now())
		s.cfg.Logger.Info("track added", "room", sess.room.ID(), "by", sess.room.Nickname(by),
			"title", track.Title, "provider", track.Provider, "queue", after.Queue)
		logChanges(s.cfg.Logger, sess.room.ID(), before, after, "add by "+sess.room.Nickname(by)+" while nothing was playing")
		sess.broadcast()
		return nil
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()
	before := sess.room.Version()
	snapBefore := sess.room.Snapshot(now)
	var err error
	switch in.Type {
	case "remove":
		err = sess.room.Remove(by, in.ItemID)
	case "vote":
		err = sess.room.Vote(by, in.ItemID)
	case "skip":
		err = sess.room.Skip(by, now)
	case "play":
		err = sess.room.Play(by, now)
	case "pause":
		err = sess.room.Pause(by, now)
	case "seek":
		err = sess.room.Seek(by, in.PositionMS, now)
	case "speaker":
		err = sess.room.SetSpeaker(by, in.On, now)
	case "report":
		_, err = sess.room.Report(by, in.ItemID, in.State, in.PositionMS, in.DurationMS, now)
	case "set_role":
		err = sess.room.SetRole(by, in.MemberID, in.Role)
	case "kick":
		if err = sess.room.Kick(by, in.MemberID, now); err == nil {
			sess.evict(in.MemberID, "kicked")
		}
	case "admit", "deny":
		return s.answer(sess, by, in.RequestID, in.Type == "admit")
	case "rotate_link":
		if by != sess.host {
			return fmt.Errorf("host only: %w", room.ErrForbidden)
		}
		sess.secret = newJoinSecret()
		sess.broadcast()
		return nil
	case "end":
		if err = sess.room.End(by); err == nil {
			s.cfg.Logger.Info("room ended", "room", sess.room.ID(), "by", sess.room.Nickname(by))
			for other := range sess.conns {
				sess.drop(other, "ended")
			}
			sess.tokens = map[[sha256.Size]byte]string{}
		}
		return err
	default:
		return fmt.Errorf("unknown command %q: %w", in.Type, room.ErrInvalid)
	}
	if err != nil {
		return err
	}
	if sess.room.Version() != before {
		s.logCommand(sess, by, in, snapBefore, sess.room.Snapshot(now))
		sess.broadcast()
	}
	return nil
}

// logCommand logs a command that changed the room, with what it changed.
func (s *Server) logCommand(sess *session, by string, in inbound, before, after room.Snapshot) {
	who := sess.room.Nickname(by)
	cause := in.Type + " by " + who
	var extra []any
	switch in.Type {
	case "report":
		if in.State != room.StateEnded {
			// A lead correcting the clock is routine. Log it only when it is large.
			if d := after.PositionMS - before.PositionMS; d > 5000 || d < -5000 {
				s.cfg.Logger.Info("clock moved by lead", "room", sess.room.ID(), "lead", who,
					"track", after.NowTitle, "from_ms", before.PositionMS, "to_ms", after.PositionMS)
			}
			return
		}
		cause = "lead " + who + " reported the end"
		extra = []any{"reason", reasonText(in.Reason), "player_at_ms", in.PositionMS, "player_duration_ms", in.DurationMS}
	case "seek":
		s.cfg.Logger.Info("seek", "room", sess.room.ID(), "by", who, "track", after.NowTitle,
			"from_ms", before.PositionMS, "to_ms", in.PositionMS)
	case "remove", "vote", "set_role", "kick":
		s.cfg.Logger.Info(in.Type, "room", sess.room.ID(), "by", who, "item", in.ItemID, "member", in.MemberID)
	}
	logChanges(s.cfg.Logger, sess.room.ID(), before, after, cause, extra...)
}
