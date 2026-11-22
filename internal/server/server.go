// Package server exposes rooms over HTTP and WebSocket.
//
// The HTTP API creates and joins rooms and hands out a session cookie. The
// WebSocket carries commands in and room state out. Every command is checked
// against the sender's role by the room itself.
package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/jamalong/internal/provider"
	"github.com/urmzd/jamalong/internal/room"
)

const (
	maxBody       = 4 << 10
	sessionTTL    = 24 * time.Hour
	idleRoomTTL   = 30 * time.Minute
	tickEvery     = 2 * time.Second
	resolveBudget = 6 * time.Second
	// maxRooms bounds memory. Creating a room past it fails.
	maxRooms = 10_000
)

// Config configures a Server.
type Config struct {
	// Resolver turns pasted links into tracks.
	Resolver provider.Resolver
	// WebDir is the built web app to serve. Empty serves the API only.
	WebDir string
	// SecureCookies marks session cookies Secure. Set it whenever the server
	// is reached over HTTPS.
	SecureCookies bool
	// Now is the clock. Nil means time.Now.
	Now func() time.Time
	// Logger receives operational logs. Nil means slog.Default.
	Logger *slog.Logger
}

// Server holds every live room.
type Server struct {
	cfg      Config
	mu       sync.Mutex
	rooms    map[string]*session
	creates  *limiter
	joins    *limiter
	upgrades *limiter
}

// session is one room plus the things only the server knows about it: the
// join secret, the session tokens and the live connections.
type session struct {
	mu     sync.Mutex
	room   *room.Room
	host   string
	secret string
	// tokens maps the SHA-256 of a session token to a member ID, so a memory
	// dump does not yield usable cookies.
	tokens    map[[sha256.Size]byte]string
	conns     map[*conn]struct{}
	idleSince time.Time
	clock     func() time.Time
}

// New returns a Server.
func New(cfg Config) *Server {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Server{
		cfg:   cfg,
		rooms: map[string]*session{},
		// Limits are per IP and a whole party shares one, so they only guard
		// against resource abuse. The join secret is what guards the room.
		creates:  newLimiter(30, time.Minute),
		joins:    newLimiter(300, time.Minute),
		upgrades: newLimiter(600, time.Minute),
	}
}

// Handler returns the HTTP handler for the API and the web app.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, map[string]bool{"ok": true}) })
	mux.HandleFunc("POST /api/rooms", s.handleCreate)
	mux.HandleFunc("POST /api/rooms/{id}/join", s.handleJoin)
	mux.HandleFunc("GET /api/rooms/{id}/session", s.handleSession)
	mux.HandleFunc("GET /api/rooms/{id}/ws", s.handleWS)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such endpoint")
	})
	if s.cfg.WebDir != "" {
		mux.Handle("/", spa(s.cfg.WebDir))
	}
	return securityHeaders(mux)
}

// Run ticks every room until ctx is done: it moves stalled queues on and
// drops rooms that have ended or sat empty.
func (s *Server) Run(ctx context.Context) {
	t := time.NewTicker(tickEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.sweep(s.cfg.Now())
		}
	}
}

func (s *Server) sweep(now time.Time) {
	s.mu.Lock()
	all := make([]*session, 0, len(s.rooms))
	for _, sess := range s.rooms {
		all = append(all, sess)
	}
	s.mu.Unlock()

	for _, sess := range all {
		sess.mu.Lock()
		if sess.room.Tick(now) {
			sess.broadcast()
		}
		switch {
		case len(sess.conns) > 0:
			sess.idleSince = time.Time{}
		case sess.idleSince.IsZero():
			sess.idleSince = now
		}
		dead := sess.room.Ended() || (!sess.idleSince.IsZero() && now.Sub(sess.idleSince) > idleRoomTTL)
		id := sess.room.ID()
		sess.mu.Unlock()
		if dead {
			s.mu.Lock()
			delete(s.rooms, id)
			s.mu.Unlock()
		}
	}
	s.creates.prune(now)
	s.joins.prune(now)
	s.upgrades.prune(now)
}

func (s *Server) lookup(id string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rooms[id]
}

// randomToken returns n random bytes in URL-safe base64.
func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("server: crypto/rand failed: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func newRoomID() string {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("server: crypto/rand failed: %v", err))
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
}

// newJoinSecret returns the 128-bit secret carried in a room link.
func newJoinSecret() string { return randomToken(16) }

func cookieName(roomID string) string { return "jam_" + roomID }

// issue creates a session token for a member and sets it as a cookie. The
// caller holds sess.mu.
func (s *Server) issue(w http.ResponseWriter, sess *session, member string) {
	token := randomToken(32)
	sess.tokens[sha256.Sum256([]byte(token))] = member
	// Secure follows Config.SecureCookies so the server also works over plain
	// HTTP on a local network.
	http.SetCookie(w, &http.Cookie{ //nolint:gosec
		Name:     cookieName(sess.room.ID()),
		Value:    token,
		Path:     "/api/rooms/" + sess.room.ID(),
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteStrictMode,
	})
}

// authed returns the room and member a request's cookie belongs to.
func (s *Server) authed(r *http.Request) (*session, string, bool) {
	sess := s.lookup(r.PathValue("id"))
	if sess == nil {
		return nil, "", false
	}
	c, err := r.Cookie(cookieName(sess.room.ID()))
	if err != nil {
		return nil, "", false
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	member, ok := sess.tokens[sha256.Sum256([]byte(c.Value))]
	if !ok || !sess.room.HasMember(member) || sess.room.Ended() {
		return nil, "", false
	}
	return sess, member, true
}

type nicknameBody struct {
	Nickname string `json:"nickname"`
	Secret   string `json:"secret"`
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var body nicknameBody
	if !s.accept(w, r, s.creates, &body) {
		return
	}
	s.mu.Lock()
	if len(s.rooms) >= maxRooms {
		s.mu.Unlock()
		writeError(w, http.StatusServiceUnavailable, "busy", "too many rooms right now, try again later")
		return
	}
	id := newRoomID()
	rm, host := room.New(id, body.Nickname)
	sess := &session{
		room: rm, host: host, secret: newJoinSecret(),
		tokens: map[[sha256.Size]byte]string{}, conns: map[*conn]struct{}{},
		clock: s.cfg.Now,
	}
	s.rooms[id] = sess
	s.mu.Unlock()

	sess.mu.Lock()
	s.issue(w, sess, host)
	sess.mu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]string{"room_id": id})
}

func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	var body nicknameBody
	if !s.accept(w, r, s.joins, &body) {
		return
	}
	// An unknown room and a wrong secret answer the same way, so room IDs
	// cannot be probed.
	const gone = "this room does not exist or the link has expired"
	sess := s.lookup(r.PathValue("id"))
	if sess == nil {
		writeError(w, http.StatusNotFound, "not_found", gone)
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if subtle.ConstantTimeCompare([]byte(body.Secret), []byte(sess.secret)) != 1 || sess.room.Ended() {
		writeError(w, http.StatusNotFound, "not_found", gone)
		return
	}
	member, err := sess.room.Join(body.Nickname)
	if err != nil {
		writeError(w, http.StatusConflict, errorCode(err), "this room is full")
		return
	}
	s.issue(w, sess, member)
	sess.broadcast()
	writeJSON(w, http.StatusCreated, map[string]string{"room_id": sess.room.ID()})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	_, member, ok := s.authed(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "join the room first")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"member_id": member})
}

// accept runs the checks shared by the state-changing endpoints: rate limit,
// same-origin, JSON content type and a bounded body.
func (s *Server) accept(w http.ResponseWriter, r *http.Request, lim *limiter, into any) bool {
	if !lim.allow(clientIP(r), s.cfg.Now()) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts, wait a minute")
		return false
	}
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "forbidden", "cross-origin request")
		return false
	}
	if ct, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";"); strings.TrimSpace(ct) != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "invalid", "send application/json")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	if err := dec.Decode(into); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "malformed request body")
		return false
	}
	return true
}

// sameOrigin rejects browser requests sent from another site. Requests with
// no Origin header are not from a browser page and carry no ambient cookie
// risk.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, room.ErrForbidden):
		return "forbidden"
	case errors.Is(err, room.ErrNotFound):
		return "not_found"
	case errors.Is(err, room.ErrFull):
		return "full"
	case errors.Is(err, room.ErrEnded):
		return "ended"
	case errors.Is(err, provider.ErrUnsupported):
		return "unsupported"
	case errors.Is(err, provider.ErrUnavailable):
		return "unavailable"
	case errors.Is(err, room.ErrInvalid):
		return "invalid"
	}
	return "internal"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}

// securityHeaders sets a content security policy that allows only this
// origin plus the YouTube, Spotify and SoundCloud players the speaker embeds.
func securityHeaders(next http.Handler) http.Handler {
	csp := strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' https://www.youtube.com https://open.spotify.com https://w.soundcloud.com",
		"frame-src https://www.youtube.com https://www.youtube-nocookie.com https://open.spotify.com https://w.soundcloud.com",
		"img-src 'self' data: https://i.ytimg.com https://*.spotifycdn.com https://i.scdn.co https://*.sndcdn.com",
		"style-src 'self' 'unsafe-inline'",
		"connect-src 'self'",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		// The YouTube player refuses to load without a referrer. This policy
		// sends the origin only, and a URL fragment is never part of it, so
		// the join secret does not leak.
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

// spa serves the built web app, and index.html for any path that is a client
// route and not a file.
func spa(dir string) http.Handler {
	root := http.Dir(dir)
	files := http.FileServer(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := path.Clean("/" + r.URL.Path)
		if isFile(root, clean) {
			files.ServeHTTP(w, r)
			return
		}
		if path.Ext(clean) != "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	})
}

// isFile reports whether name is a regular file under root. http.Dir refuses
// paths that escape it.
func isFile(root http.Dir, name string) bool {
	f, err := root.Open(name)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	return err == nil && !info.IsDir()
}
