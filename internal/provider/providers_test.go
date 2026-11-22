package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/urmzd/jamalong/internal/room"
)

const spotifyTestID = "4PTG3Z6ehGkBFwjybzWkR8"

func TestSpotifyTrackID(t *testing.T) {
	ok := []string{
		"https://open.spotify.com/track/" + spotifyTestID,
		"https://open.spotify.com/track/" + spotifyTestID + "?si=abc123",
		"https://open.spotify.com/intl-fr/track/" + spotifyTestID,
		"https://open.spotify.com/intl-pt-br/track/" + spotifyTestID,
		"https://open.spotify.com/embed/track/" + spotifyTestID,
		"spotify:track:" + spotifyTestID,
		"  https://open.spotify.com/track/" + spotifyTestID + "  ",
	}
	for _, link := range ok {
		if id, found := SpotifyTrackID(link); !found || id != spotifyTestID {
			t.Errorf("SpotifyTrackID(%q) = %q, %v", link, id, found)
		}
	}
	bad := []string{
		"",
		spotifyTestID,
		"https://open.spotify.com/album/" + spotifyTestID,
		"https://open.spotify.com/playlist/" + spotifyTestID,
		"https://open.spotify.com/track/short",
		"https://open.spotify.com.evil.example/track/" + spotifyTestID,
		"https://evil.example/?u=https://open.spotify.com/track/" + spotifyTestID,
		"spotify:album:" + spotifyTestID,
		"spotify:track:" + spotifyTestID + "x",
		"ftp://open.spotify.com/track/" + spotifyTestID,
	}
	for _, link := range bad {
		if id, found := SpotifyTrackID(link); found {
			t.Errorf("SpotifyTrackID(%q) accepted %q", link, id)
		}
	}
}

func spotifyFake(t *testing.T, oembed int, page string) Spotify {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/oembed", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("url"); got != "https://open.spotify.com/track/"+spotifyTestID {
			t.Errorf("oembed url = %q", got)
		}
		if oembed != http.StatusOK {
			w.WriteHeader(oembed)
			return
		}
		_, _ = w.Write([]byte(`{"title":"Never Gonna Give You Up","thumbnail_url":"https://image-cdn-ak.spotifycdn.com/image/abc"}`))
	})
	mux.HandleFunc("/embed/track/", func(w http.ResponseWriter, _ *http.Request) {
		if page == "" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(page))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return Spotify{Client: srv.Client(), OEmbed: srv.URL + "/oembed", Embed: srv.URL + "/embed/track/"}
}

func nextDataPage(entity string) string {
	return `<html><script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"state":{"data":{"entity":` +
		entity + `}}}}}</script></html>`
}

func TestSpotifyResolve(t *testing.T) {
	page := nextDataPage(`{"artists":[{"name":"Rick Astley"},{"name":"Guest"}],"duration":213573,"isPlayable":true}`)
	got, err := spotifyFake(t, http.StatusOK, page).Resolve(context.Background(), "https://open.spotify.com/track/"+spotifyTestID)
	if err != nil {
		t.Fatal(err)
	}
	want := room.Track{
		Provider:   SpotifyName,
		ProviderID: spotifyTestID,
		Title:      "Never Gonna Give You Up",
		Artist:     "Rick Astley, Guest",
		DurationMS: 213573,
		ArtworkURL: "https://image-cdn-ak.spotifycdn.com/image/abc",
	}
	if got != want {
		t.Errorf("Resolve = %+v, want %+v", got, want)
	}
}

func TestSpotifyResolveWithoutEmbedPage(t *testing.T) {
	got, err := spotifyFake(t, http.StatusOK, "").Resolve(context.Background(), "spotify:track:"+spotifyTestID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title == "" || got.Artist != "" || got.DurationMS != 0 {
		t.Errorf("Resolve = %+v, want a title and no artist or length", got)
	}
}

func TestSpotifyResolveErrors(t *testing.T) {
	link := "https://open.spotify.com/track/" + spotifyTestID
	if _, err := spotifyFake(t, http.StatusNotFound, "").Resolve(context.Background(), link); !errors.Is(err, ErrUnavailable) {
		t.Errorf("missing track: err = %v, want ErrUnavailable", err)
	}
	blocked := nextDataPage(`{"artists":[],"duration":1000,"isPlayable":false}`)
	if _, err := spotifyFake(t, http.StatusOK, blocked).Resolve(context.Background(), link); !errors.Is(err, ErrUnavailable) {
		t.Errorf("unplayable track: err = %v, want ErrUnavailable", err)
	}
	if _, err := (Spotify{}).Resolve(context.Background(), "https://open.spotify.com/album/"+spotifyTestID); !errors.Is(err, ErrUnsupported) {
		t.Errorf("album: err = %v, want ErrUnsupported", err)
	}
}

func TestSoundCloudTrackURL(t *testing.T) {
	ok := map[string]string{
		"https://soundcloud.com/forss/flickermood":                 "https://soundcloud.com/forss/flickermood",
		"https://m.soundcloud.com/forss/flickermood?si=x&utm=y":    "https://soundcloud.com/forss/flickermood",
		"https://www.soundcloud.com/forss/flickermood/":            "https://soundcloud.com/forss/flickermood",
		"  http://soundcloud.com/some-user_1/a-track-name-2  ":     "https://soundcloud.com/some-user_1/a-track-name-2",
		"https://soundcloud.com/forss/flickermood#t=0:30":          "https://soundcloud.com/forss/flickermood",
		"https://soundcloud.com/Forss/FlickerMood?in=forss/sets/x": "https://soundcloud.com/Forss/FlickerMood",
	}
	for link, want := range ok {
		if got, found := SoundCloudTrackURL(link); !found || got != want {
			t.Errorf("SoundCloudTrackURL(%q) = %q, %v; want %q", link, got, found, want)
		}
	}
	bad := []string{
		"",
		"https://soundcloud.com/forss",
		"https://soundcloud.com/forss/sets",
		"https://soundcloud.com/forss/sets/soulhack",
		"https://soundcloud.com/forss/likes",
		"https://soundcloud.com/discover/sets",
		"https://soundcloud.com/forss/flickermood/comments",
		"https://soundcloud.com.evil.example/forss/flickermood",
		"https://evil.example/forss/flickermood",
		"https://soundcloud.com/forss/flicker%2Fmood",
		"ftp://soundcloud.com/forss/flickermood",
	}
	for _, link := range bad {
		if got, found := SoundCloudTrackURL(link); found {
			t.Errorf("SoundCloudTrackURL(%q) accepted %q", link, got)
		}
	}
}

func soundCloudFake(t *testing.T, html string) (SoundCloud, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/oembed", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("url"); got != "https://soundcloud.com/forss/flickermood" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"title":"Flickermood by Forss","author_name":"Forss",` +
			`"thumbnail_url":"https://i1.sndcdn.com/artworks-1-t500x500.jpg","html":` + html + `}`))
	})
	mux.HandleFunc("/s/", func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/s/") {
		case "good":
			http.Redirect(w, r, "https://soundcloud.com/forss/flickermood?si=1", http.StatusFound)
		case "set":
			http.Redirect(w, r, "https://soundcloud.com/forss/sets/soulhack", http.StatusFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return SoundCloud{Client: srv.Client(), OEmbed: srv.URL + "/oembed", Short: srv.URL + "/s/"}, srv
}

const soundCloudWidget = `"<iframe src=\"https://w.soundcloud.com/player/?visual=true&url=https%3A%2F%2Fapi.soundcloud.com%2Ftracks%2F293&show_artwork=true\"></iframe>"`

func TestSoundCloudResolve(t *testing.T) {
	sc, _ := soundCloudFake(t, soundCloudWidget)
	want := room.Track{
		Provider:   SoundCloudName,
		ProviderID: "293",
		Title:      "Flickermood",
		Artist:     "Forss",
		ArtworkURL: "https://i1.sndcdn.com/artworks-1-t500x500.jpg",
	}
	for _, link := range []string{"https://soundcloud.com/forss/flickermood", "https://on.soundcloud.com/good"} {
		got, err := sc.Resolve(context.Background(), link)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", link, err)
		}
		if got != want {
			t.Errorf("Resolve(%q) = %+v, want %+v", link, got, want)
		}
	}
}

func TestSoundCloudResolveErrors(t *testing.T) {
	sc, _ := soundCloudFake(t, `"<iframe src=\"https://w.soundcloud.com/player/?url=https%3A%2F%2Fapi.soundcloud.com%2Fplaylists%2F9\"></iframe>"`)
	if _, err := sc.Resolve(context.Background(), "https://soundcloud.com/forss/flickermood"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("no track id: err = %v, want ErrUnavailable", err)
	}
	if _, err := sc.Resolve(context.Background(), "https://soundcloud.com/forss/gone"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("missing track: err = %v, want ErrUnavailable", err)
	}
	if _, err := sc.Resolve(context.Background(), "https://on.soundcloud.com/set"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("short link to a set: err = %v, want ErrUnsupported", err)
	}
	if _, err := sc.Resolve(context.Background(), "https://on.soundcloud.com/missing"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("dead short link: err = %v, want ErrUnavailable", err)
	}
	if _, err := sc.Resolve(context.Background(), "https://soundcloud.com/forss/sets/soulhack"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("set: err = %v, want ErrUnsupported", err)
	}
}

type stubResolver struct {
	track room.Track
	err   error
}

func (s stubResolver) Resolve(context.Context, string) (room.Track, error) { return s.track, s.err }

func TestChain(t *testing.T) {
	a := room.Track{Title: "a"}
	c := Chain{stubResolver{err: ErrUnsupported}, stubResolver{track: a}, stubResolver{err: errors.New("unreached")}}
	if got, err := c.Resolve(context.Background(), "x"); err != nil || got != a {
		t.Errorf("Chain = %+v, %v", got, err)
	}
	c = Chain{stubResolver{err: ErrUnsupported}, stubResolver{err: ErrUnavailable}}
	if _, err := c.Resolve(context.Background(), "x"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Chain err = %v, want ErrUnavailable", err)
	}
	if _, err := (Chain{stubResolver{err: ErrUnsupported}}).Resolve(context.Background(), "x"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Chain err = %v, want ErrUnsupported", err)
	}
}

func TestSafeImage(t *testing.T) {
	cases := map[string]string{
		"https://i1.sndcdn.com/a.jpg":          "https://i1.sndcdn.com/a.jpg",
		"https://sndcdn.com/a.jpg":             "https://sndcdn.com/a.jpg",
		"http://i1.sndcdn.com/a.jpg":           "",
		"https://evilsndcdn.com/a.jpg":         "",
		"https://sndcdn.com.evil.example/a":    "",
		"https://user@i1.sndcdn.com/a.jpg":     "",
		"javascript:alert(1)//i1.sndcdn.com/a": "",
	}
	for in, want := range cases {
		if got := safeImage(in, "sndcdn.com"); got != want {
			t.Errorf("safeImage(%q) = %q, want %q", in, got, want)
		}
	}
}
