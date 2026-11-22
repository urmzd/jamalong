package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestOpenRoomsAndJoinRequests(t *testing.T) {
	base, host, roomID, _ := start(t)
	visitor := newClient(t, base)

	// The room is listed while someone is connected.
	res, err := visitor.http.Get(base + "/api/rooms")
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Rooms []openRoom `json:"rooms"`
	}
	err = json.NewDecoder(res.Body).Decode(&list)
	_ = res.Body.Close()
	if err != nil || len(list.Rooms) != 1 || list.Rooms[0].ID != roomID || list.Rooms[0].Host != "Host" {
		t.Fatalf("list = %+v, %v", list, err)
	}

	// A visitor asks. The host sees the request; the visitor waits.
	status, out := visitor.post("/api/rooms/"+roomID+"/requests", map[string]string{"nickname": "Ada"})
	if status != http.StatusCreated || len(out["request"]) < 40 {
		t.Fatalf("request = %d %v", status, out)
	}
	token := out["request"]
	f := host.until("request shown", func(f frame) bool { return isState(f) && len(f.Requests) == 1 })
	if f.Requests[0].Nickname != "Ada" {
		t.Fatalf("requests = %+v", f.Requests)
	}
	if status, out := visitor.post("/api/rooms/"+roomID+"/requests/claim", map[string]string{"request": token}); status != http.StatusAccepted || out["status"] != "waiting" {
		t.Fatalf("claim before answer = %d %v", status, out)
	}
	if err := visitor.dial(roomID, nil); err == nil {
		t.Fatal("socket opened before the host answered")
	}

	// A wrong token learns nothing.
	if status, _ := visitor.post("/api/rooms/"+roomID+"/requests/claim", map[string]string{"request": "nope"}); status != http.StatusNotFound {
		t.Fatalf("wrong token = %d, want 404", status)
	}

	// The host admits; the visitor's next poll gets a session.
	host.send(map[string]any{"type": "admit", "request_id": f.Requests[0].ID})
	host.until("request cleared", func(f frame) bool { return isState(f) && len(f.Requests) == 0 && len(f.Room.Members) == 2 })
	if status, out := visitor.post("/api/rooms/"+roomID+"/requests/claim", map[string]string{"request": token}); status != http.StatusOK || out["status"] != "admitted" {
		t.Fatalf("claim after admit = %d %v", status, out)
	}
	if err := visitor.dial(roomID, nil); err != nil {
		t.Fatalf("admitted visitor cannot connect: %v", err)
	}
	// A request token works once.
	if status, _ := visitor.post("/api/rooms/"+roomID+"/requests/claim", map[string]string{"request": token}); status != http.StatusNotFound {
		t.Fatalf("second claim = %d, want 404", status)
	}

	// Only the host answers requests.
	other := newClient(t, base)
	_, out = other.post("/api/rooms/"+roomID+"/requests", map[string]string{"nickname": "Eve"})
	f = host.until("second request", func(f frame) bool { return isState(f) && len(f.Requests) == 1 })
	visitor.send(map[string]any{"type": "admit", "request_id": f.Requests[0].ID})
	if g := visitor.until("forbidden", func(f frame) bool { return f.Type == "error" }); g.Code != "forbidden" {
		t.Fatalf("guest admit = %+v", g)
	}
	host.send(map[string]any{"type": "deny", "request_id": f.Requests[0].ID})
	host.until("denied", func(f frame) bool { return isState(f) && len(f.Requests) == 0 })
	if status, res := other.post("/api/rooms/"+roomID+"/requests/claim", map[string]string{"request": out["request"]}); status != http.StatusForbidden || res["code"] != "denied" {
		t.Fatalf("claim after deny = %d %v", status, res)
	}

	// Another site cannot ask on a visitor's behalf.
	if status, _ := other.post("/api/rooms/"+roomID+"/requests", map[string]string{"nickname": "x"}, "Origin", "https://evil.example"); status != http.StatusForbidden {
		t.Fatalf("cross-origin request = %d, want 403", status)
	}
}
