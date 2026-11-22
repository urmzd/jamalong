package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/urmzd/jamalong/internal/room"
)

// YouTubeName is the provider name stored on YouTube tracks.
const YouTubeName = "youtube"

const youTubeOEmbed = "https://www.youtube.com/oembed"

var youTubeID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// YouTube resolves YouTube and YouTube Music links through the public oEmbed
// endpoint, which needs no API key. oEmbed does not report duration, so the
// lead speaker fills that in once its player has loaded the video.
type YouTube struct {
	// Client makes the oEmbed request. Nil means a client with a 5s timeout.
	Client *http.Client
	// Endpoint overrides the oEmbed URL in tests.
	Endpoint string
}

// YouTubeVideoID extracts the video ID from a YouTube link. It accepts watch,
// short, embed, live and youtu.be forms.
func YouTubeVideoID(link string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	path := strings.Trim(u.Path, "/")
	id := ""
	switch host {
	case "youtu.be":
		id = path
	case "youtube.com", "m.youtube.com", "music.youtube.com":
		kind, rest, _ := strings.Cut(path, "/")
		switch kind {
		case "watch":
			id = u.Query().Get("v")
		case "shorts", "embed", "live":
			id = rest
		}
	}
	if !youTubeID.MatchString(id) {
		return "", false
	}
	return id, true
}

// Resolve implements Resolver.
func (y YouTube) Resolve(ctx context.Context, link string) (room.Track, error) {
	id, ok := YouTubeVideoID(link)
	if !ok {
		return room.Track{}, ErrUnsupported
	}
	endpoint := y.Endpoint
	if endpoint == "" {
		endpoint = youTubeOEmbed
	}
	// The request URL is built from the validated ID only, never from the
	// member's link.
	q := url.Values{"format": {"json"}, "url": {"https://www.youtube.com/watch?v=" + id}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return room.Track{}, fmt.Errorf("youtube: %w", err)
	}
	client := y.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return room.Track{}, fmt.Errorf("youtube: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		// 401 and 403 mean the owner disabled embedding. 400 and 404 mean
		// the video is gone or private.
		return room.Track{}, ErrUnavailable
	}
	var meta struct {
		Title  string `json:"title"`
		Author string `json:"author_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&meta); err != nil {
		return room.Track{}, fmt.Errorf("youtube: %w", err)
	}
	if strings.TrimSpace(meta.Title) == "" {
		return room.Track{}, ErrUnavailable
	}
	return room.Track{
		Provider:   YouTubeName,
		ProviderID: id,
		Title:      clip(meta.Title, 200),
		Artist:     clip(strings.TrimSuffix(meta.Author, " - Topic"), 200),
		ArtworkURL: "https://i.ytimg.com/vi/" + id + "/mqdefault.jpg",
	}, nil
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}
