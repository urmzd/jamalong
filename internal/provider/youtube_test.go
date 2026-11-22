package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestYouTubeVideoID(t *testing.T) {
	ok := []string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://youtube.com/watch?v=dQw4w9WgXcQ&list=PL123&t=10s",
		"https://music.youtube.com/watch?v=dQw4w9WgXcQ&si=abc",
		"https://m.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://youtu.be/dQw4w9WgXcQ?t=4",
		"https://www.youtube.com/shorts/dQw4w9WgXcQ",
		"https://www.youtube.com/embed/dQw4w9WgXcQ",
		"  https://www.youtube.com/live/dQw4w9WgXcQ  ",
	}
	for _, link := range ok {
		if id, found := YouTubeVideoID(link); !found || id != "dQw4w9WgXcQ" {
			t.Errorf("YouTubeVideoID(%q) = %q, %v", link, id, found)
		}
	}
	bad := []string{
		"",
		"dQw4w9WgXcQ",
		"javascript:alert(1)",
		"ftp://youtube.com/watch?v=dQw4w9WgXcQ",
		"https://youtube.com.evil.example/watch?v=dQw4w9WgXcQ",
		"https://evil.example/?u=https://youtu.be/dQw4w9WgXcQ",
		"https://www.youtube.com/watch?v=short",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ%2F..%2Fx",
		"https://www.youtube.com/playlist?list=PL123",
	}
	for _, link := range bad {
		if id, found := YouTubeVideoID(link); found {
			t.Errorf("YouTubeVideoID(%q) accepted %q", link, id)
		}
	}
}

func TestYouTubeResolve(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("url"); got != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
			t.Errorf("oembed url = %q", got)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"title":" Never Gonna Give You Up ","author_name":"Rick Astley - Topic","thumbnail_url":"https://evil.example/x.jpg"}`))
	}))
	defer srv.Close()
	y := YouTube{Endpoint: srv.URL}

	tr, err := y.Resolve(context.Background(), "https://youtu.be/dQw4w9WgXcQ?t=4")
	if err != nil {
		t.Fatal(err)
	}
	if tr.Provider != "youtube" || tr.ProviderID != "dQw4w9WgXcQ" || tr.Title != "Never Gonna Give You Up" ||
		tr.Artist != "Rick Astley" || tr.ArtworkURL != "https://i.ytimg.com/vi/dQw4w9WgXcQ/mqdefault.jpg" {
		t.Fatalf("track = %+v", tr)
	}

	status = http.StatusUnauthorized
	if _, err := y.Resolve(context.Background(), "https://youtu.be/dQw4w9WgXcQ"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("embedding disabled err = %v", err)
	}
	if _, err := y.Resolve(context.Background(), "https://example.com/a"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("foreign link err = %v", err)
	}
}
