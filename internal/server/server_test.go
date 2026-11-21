package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/urmzd/jamalong/internal/provider"
	"github.com/urmzd/jamalong/internal/room"
)

type fakeResolver struct{}

func (fakeResolver) Resolve(_ context.Context, link string) (room.Track, error) {
	if !strings.HasPrefix(link, "ok:") {
		return room.Track{}, provider.ErrUnsupported
	}
	title := strings.TrimPrefix(link, "ok:")
	return room.Track{Provider: "youtube", ProviderID: title, Title: title}, nil
}

type client struct {
	t    *testing.T
	base string
	http *http.Client
	ws   *websocket.Conn
}

func newClient(t *testing.T, base string) *client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &client{t: t, base: base, http: &http.Client{Jar: jar}}
}

func (c *client) post(path string, body any, header ...string) (int, map[string]string) {
	c.t.Helper()
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, c.base+path, bytes.NewReader(b))
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out := map[string]string{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (c *client) dial(roomID string, header http.Header) error {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, resp, err := websocket.Dial(ctx, c.base+"/api/rooms/"+roomID+"/ws", &websocket.DialOptions{
		HTTPClient: c.http, HTTPHeader: header,
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return err
	}
	c.ws = ws
	c.t.Cleanup(func() { _ = ws.CloseNow() })
	return nil
}

func (c *client) send(v any) {
	c.t.Helper()
	b, _ := json.Marshal(v)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.ws.Write(ctx, websocket.MessageText, b); err != nil {
		c.t.Fatal(err)
	}
}

type frame struct {
	Type       string    `json:"type"`
	Room       room.View `json:"room"`
	JoinSecret string    `json:"join_secret"`
	Code       string    `json:"code"`
	Ref        string    `json:"ref"`
}

// until reads frames until one satisfies ok, and returns it.
func (c *client) until(what string, ok func(frame) bool) frame {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, data, err := c.ws.Read(ctx)
		if err != nil {
			c.t.Fatalf("waiting for %s: %v", what, err)
		}
		var f frame
		if err := json.Unmarshal(data, &f); err != nil {
			c.t.Fatal(err)
		}
		if ok(f) {
			return f
		}
	}
}

// closed reads until the server closes the socket, and returns the reason.
func (c *client) closed() string {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		if _, _, err := c.ws.Read(ctx); err != nil {
			var ce websocket.CloseError
			if errors.As(err, &ce) {
				return ce.Reason
			}
			c.t.Fatalf("socket did not close cleanly: %v", err)
		}
	}
}

func isState(f frame) bool { return f.Type == "state" }

func start(t *testing.T) (base string, host *client, roomID, secret string) {
	t.Helper()
	srv := httptest.NewServer(New(Config{Resolver: fakeResolver{}}).Handler())
	t.Cleanup(srv.Close)
	host = newClient(t, srv.URL)
	status, out := host.post("/api/rooms", map[string]string{"nickname": "Host"})
	if status != http.StatusCreated || out["room_id"] == "" {
		t.Fatalf("create = %d %v", status, out)
	}
	roomID = out["room_id"]
	if err := host.dial(roomID, nil); err != nil {
		t.Fatal(err)
	}
	f := host.until("first state", isState)
	if len(f.JoinSecret) < 22 {
		t.Fatalf("host join secret = %q, want 128 bits of base64url", f.JoinSecret)
	}
	return srv.URL, host, roomID, f.JoinSecret
}

func join(t *testing.T, base, roomID, secret, name string) *client {
	t.Helper()
	c := newClient(t, base)
	if status, out := c.post("/api/rooms/"+roomID+"/join", map[string]string{"secret": secret, "nickname": name}); status != http.StatusCreated {
		t.Fatalf("join = %d %v", status, out)
	}
	if err := c.dial(roomID, nil); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestJoinNeedsTheSecret(t *testing.T) {
	base, _, roomID, secret := start(t)
	stranger := newClient(t, base)

	for name, tc := range map[string]struct{ room, secret string }{
		"wrong secret": {roomID, "AAAAAAAAAAAAAAAAAAAAAA"},
		"empty secret": {roomID, ""},
		"unknown room": {"nosuchroom", secret},
	} {
		status, out := stranger.post("/api/rooms/"+tc.room+"/join", map[string]string{"secret": tc.secret})
		if status != http.StatusNotFound || out["code"] != "not_found" {
			t.Errorf("%s: join = %d %v, want the same 404 for all", name, status, out)
		}
	}
	// No cookie, no socket.
	if err := stranger.dial(roomID, nil); err == nil {
		t.Fatal("socket opened without a session")
	}
	// Another site cannot post on a visitor's behalf.
	if status, _ := stranger.post("/api/rooms/"+roomID+"/join", map[string]string{"secret": secret}, "Origin", "https://evil.example"); status != http.StatusForbidden {
		t.Fatalf("cross-origin join = %d, want 403", status)
	}
	// Nor open a socket with the visitor's cookie.
	guest := newClient(t, base)
	guest.post("/api/rooms/"+roomID+"/join", map[string]string{"secret": secret})
	if err := guest.dial(roomID, http.Header{"Origin": {"https://evil.example"}}); err == nil {
		t.Fatal("cross-origin socket opened")
	}
}

func TestSharedQueueAndRoles(t *testing.T) {
	base, host, roomID, secret := start(t)
	guest := join(t, base, roomID, secret, "Ada")

	// Only the host is told the join secret.
	if f := guest.until("guest state", isState); f.JoinSecret != "" {
		t.Fatal("a guest was sent the join secret")
	}

	host.send(map[string]any{"type": "speaker", "on": true})
	host.until("host is lead", func(f frame) bool { return isState(f) && f.Room.Lead == f.Room.You })
	guest.send(map[string]any{"type": "add_url", "url": "ok:first"})
	playing := func(title string) func(frame) bool {
		return func(f frame) bool { return isState(f) && f.Room.Now != nil && f.Room.Now.Item.Track.Title == title }
	}
	// Both devices converge on the same queue.
	hf := host.until("host sees track", playing("first"))
	gf := guest.until("guest sees track", playing("first"))
	if hf.Room.Now.Item.ID != gf.Room.Now.Item.ID || hf.Room.Lead != hf.Room.You {
		t.Fatalf("host %+v guest %+v", hf.Room, gf.Room)
	}

	// A guest cannot control playback, and is told why.
	guest.send(map[string]any{"type": "pause"})
	if f := guest.until("forbidden", func(f frame) bool { return f.Type == "error" }); f.Code != "forbidden" || f.Ref != "pause" {
		t.Fatalf("guest pause = %+v", f)
	}
	guest.send(map[string]any{"type": "add_url", "url": "https://example.com/x"})
	if f := guest.until("unsupported", func(f frame) bool { return f.Type == "error" }); f.Code != "unsupported" {
		t.Fatalf("bad link = %+v", f)
	}

	// The lead speaker's end-of-track report moves the queue on.
	guest.send(map[string]any{"type": "add_url", "url": "ok:second"})
	host.until("second queued", func(f frame) bool { return isState(f) && len(f.Room.Queue) == 1 })
	host.send(map[string]any{"type": "report", "item_id": hf.Room.Now.Item.ID, "state": "ended"})
	guest.until("advance", playing("second"))
}

func TestRotateLinkKickAndEnd(t *testing.T) {
	base, host, roomID, secret := start(t)
	guest := join(t, base, roomID, secret, "Ada")
	gf := guest.until("guest state", isState)

	// A guest cannot rotate the link.
	guest.send(map[string]any{"type": "rotate_link"})
	guest.until("forbidden", func(f frame) bool { return f.Type == "error" && f.Code == "forbidden" })

	host.send(map[string]any{"type": "rotate_link"})
	fresh := host.until("new secret", func(f frame) bool { return isState(f) && f.JoinSecret != secret }).JoinSecret
	late := newClient(t, base)
	if status, _ := late.post("/api/rooms/"+roomID+"/join", map[string]string{"secret": secret}); status != http.StatusNotFound {
		t.Fatalf("old link join = %d, want 404", status)
	}

	// A kick closes the socket and kills the session.
	host.send(map[string]any{"type": "kick", "member_id": gf.Room.You})
	if reason := guest.closed(); reason != "kicked" {
		t.Fatalf("close reason = %q, want kicked", reason)
	}
	if err := guest.dial(roomID, nil); err == nil {
		t.Fatal("kicked member reconnected")
	}

	// Ending the room closes everyone and refuses new joins.
	second := join(t, base, roomID, fresh, "Bo")
	host.send(map[string]any{"type": "end"})
	if reason := second.closed(); reason != "ended" {
		t.Fatalf("close reason = %q, want ended", reason)
	}
	if status, _ := late.post("/api/rooms/"+roomID+"/join", map[string]string{"secret": fresh}); status != http.StatusNotFound {
		t.Fatalf("join after end = %d, want 404", status)
	}
}

func TestSweepDropsEndedAndIdleRooms(t *testing.T) {
	now := time.Now()
	s := New(Config{Now: func() time.Time { return now }})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	c := newClient(t, srv.URL)
	_, out := c.post("/api/rooms", map[string]string{"nickname": "Host"})

	s.sweep(now)
	now = now.Add(idleRoomTTL - time.Minute)
	s.sweep(now)
	if s.lookup(out["room_id"]) == nil {
		t.Fatal("room dropped before the idle limit")
	}
	now = now.Add(2 * time.Minute)
	s.sweep(now)
	if s.lookup(out["room_id"]) != nil {
		t.Fatal("idle room was kept")
	}
}

func TestLimiter(t *testing.T) {
	now := time.Now()
	l := newLimiter(2, time.Minute)
	got := []bool{l.allow("a", now), l.allow("a", now), l.allow("a", now)}
	if !got[0] || !got[1] || got[2] {
		t.Fatalf("limit of 2 gave %v", got)
	}
	if !l.allow("b", now) {
		t.Fatal("keys share a bucket")
	}
	if !l.allow("a", now.Add(time.Minute)) {
		t.Fatal("window did not reset")
	}
	tk := &tokens{level: 1, burst: 1, rate: 1}
	took := []bool{tk.take(now), tk.take(now), tk.take(now.Add(time.Second))}
	if !took[0] || took[1] || !took[2] {
		t.Fatalf("token bucket gave %v", took)
	}
}
