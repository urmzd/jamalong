package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/urmzd/jamalong/internal/room"
)

// SpotifyName is the provider name stored on Spotify tracks.
const SpotifyName = "spotify"

const (
	spotifyOEmbed = "https://open.spotify.com/oembed"
	spotifyEmbed  = "https://open.spotify.com/embed/track/"
)

var (
	spotifyID     = regexp.MustCompile(`^[A-Za-z0-9]{22}$`)
	spotifyLocale = regexp.MustCompile(`^intl-[a-z]{2}(-[a-z]{2})?$`)
	nextData      = regexp.MustCompile(`(?s)<script id="__NEXT_DATA__" type="application/json">(.*?)</script>`)
)

// Spotify resolves Spotify track links through the public oEmbed endpoint,
// which needs no client ID. Speakers play the track in the Spotify embed:
// a listener signed in to Spotify in that browser hears the full track, and
// anyone else hears a preview.
type Spotify struct {
	// Client makes the requests. Nil means a client with a 5s timeout.
	Client *http.Client
	// OEmbed and Embed override the endpoints in tests.
	OEmbed string
	Embed  string
}

// SpotifyTrackID extracts the track ID from an open.spotify.com track link
// or a spotify:track: URI.
func SpotifyTrackID(link string) (string, bool) {
	link = strings.TrimSpace(link)
	if id, ok := strings.CutPrefix(link, "spotify:track:"); ok {
		return id, spotifyID.MatchString(id)
	}
	u, ok := webURL(link)
	if !ok {
		return "", false
	}
	if strings.ToLower(u.Hostname()) != "open.spotify.com" {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) > 0 && spotifyLocale.MatchString(parts[0]) {
		parts = parts[1:]
	}
	if len(parts) == 3 && parts[0] == "embed" {
		parts = parts[1:]
	}
	if len(parts) != 2 || parts[0] != "track" || !spotifyID.MatchString(parts[1]) {
		return "", false
	}
	return parts[1], true
}

// Resolve implements Resolver.
func (s Spotify) Resolve(ctx context.Context, link string) (room.Track, error) {
	id, ok := SpotifyTrackID(link)
	if !ok {
		return room.Track{}, ErrUnsupported
	}
	endpoint := s.OEmbed
	if endpoint == "" {
		endpoint = spotifyOEmbed
	}
	// Request URLs are built from the validated ID only.
	q := url.Values{oembedURL: {"https://open.spotify.com/track/" + id}}
	var meta struct {
		Title     string `json:"title"`
		Thumbnail string `json:"thumbnail_url"`
	}
	if err := getJSON(ctx, s.Client, endpoint+"?"+q.Encode(), &meta); err != nil {
		if errors.Is(err, ErrUnavailable) {
			return room.Track{}, err
		}
		return room.Track{}, fmt.Errorf("spotify: %w", err)
	}
	if strings.TrimSpace(meta.Title) == "" {
		return room.Track{}, ErrUnavailable
	}
	track := room.Track{
		Provider:   SpotifyName,
		ProviderID: id,
		Title:      clip(meta.Title, 200),
		ArtworkURL: safeImage(meta.Thumbnail, "spotifycdn.com", "scdn.co"),
	}
	// oEmbed leaves out the artist and the length. The embed page has both.
	// It is not a documented API, so a failure here keeps the track and
	// leaves those two fields empty; the lead speaker reports the length.
	if extra, err := s.embedDetails(ctx, id); err == nil {
		if !extra.playable {
			return room.Track{}, ErrUnavailable
		}
		track.Artist = clip(extra.artist, 200)
		track.DurationMS = extra.durationMS
	}
	return track, nil
}

type spotifyDetails struct {
	artist     string
	durationMS int64
	playable   bool
}

func (s Spotify) embedDetails(ctx context.Context, id string) (spotifyDetails, error) {
	base := s.Embed
	if base == "" {
		base = spotifyEmbed
	}
	body, err := get(ctx, s.Client, base+id)
	if err != nil {
		return spotifyDetails{}, err
	}
	m := nextData.FindSubmatch(body)
	if m == nil {
		return spotifyDetails{}, fmt.Errorf("spotify: embed page has no track data")
	}
	var page struct {
		Props struct {
			PageProps struct {
				State struct {
					Data struct {
						Entity struct {
							Artists []struct {
								Name string `json:"name"`
							} `json:"artists"`
							Duration   int64 `json:"duration"`
							IsPlayable *bool `json:"isPlayable"`
						} `json:"entity"`
					} `json:"data"`
				} `json:"state"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.NewDecoder(bytes.NewReader(m[1])).Decode(&page); err != nil {
		return spotifyDetails{}, fmt.Errorf("spotify: %w", err)
	}
	e := page.Props.PageProps.State.Data.Entity
	names := make([]string, 0, len(e.Artists))
	for _, a := range e.Artists {
		if n := strings.TrimSpace(a.Name); n != "" {
			names = append(names, n)
		}
	}
	return spotifyDetails{
		artist:     strings.Join(names, ", "),
		durationMS: max(e.Duration, 0),
		playable:   e.IsPlayable == nil || *e.IsPlayable,
	}, nil
}
