package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/urmzd/jamalong/internal/room"
)

// SoundCloudName is the provider name stored on SoundCloud tracks.
const SoundCloudName = "soundcloud"

const (
	soundCloudOEmbed = "https://soundcloud.com/oembed"
	soundCloudShort  = "https://on.soundcloud.com/"
)

var (
	soundCloudSlug  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,255}$`)
	soundCloudCode  = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)
	soundCloudTrack = regexp.MustCompile(`api\.soundcloud\.com(?:%2F|/)tracks(?:%2F|/)(\d{1,20})`)
	// Profile pages that share the user/track shape but are not tracks.
	soundCloudPages = map[string]bool{
		"albums": true, "comments": true, "followers": true, "following": true,
		"likes": true, "popular-tracks": true, "reposts": true, "sets": true,
		"spotlight": true, "tracks": true,
	}
	soundCloudRoots = map[string]bool{
		"charts": true, "discover": true, "feed": true, "messages": true, "pages": true,
		"search": true, "settings": true, "stations": true, "upload": true, "you": true,
	}
)

// SoundCloud resolves SoundCloud track links through the public oEmbed
// endpoint, which needs no app registration. Speakers play the track in the
// SoundCloud widget. Go+ tracks play as a preview.
type SoundCloud struct {
	// Client makes the requests. Nil means a client with a 5s timeout.
	Client *http.Client
	// OEmbed and Short override the endpoints in tests.
	OEmbed string
	Short  string
}

// SoundCloudTrackURL returns the canonical https://soundcloud.com/user/track
// form of a track link, or false when the link is not a track.
func SoundCloudTrackURL(link string) (string, bool) {
	u, ok := webURL(link)
	if !ok {
		return "", false
	}
	switch strings.ToLower(u.Hostname()) {
	case "soundcloud.com", "www.soundcloud.com", "m.soundcloud.com":
	default:
		return "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 {
		return "", false
	}
	user, slug := parts[0], parts[1]
	if !soundCloudSlug.MatchString(user) || !soundCloudSlug.MatchString(slug) {
		return "", false
	}
	if soundCloudRoots[strings.ToLower(user)] || soundCloudPages[strings.ToLower(slug)] {
		return "", false
	}
	return "https://soundcloud.com/" + user + "/" + slug, true
}

// soundCloudShortCode returns the code of an on.soundcloud.com share link.
func soundCloudShortCode(link string) (string, bool) {
	u, ok := webURL(link)
	if !ok {
		return "", false
	}
	if strings.ToLower(u.Hostname()) != "on.soundcloud.com" {
		return "", false
	}
	code := strings.Trim(u.Path, "/")
	return code, soundCloudCode.MatchString(code)
}

// Resolve implements Resolver.
func (s SoundCloud) Resolve(ctx context.Context, link string) (room.Track, error) {
	canonical, ok := SoundCloudTrackURL(link)
	if !ok {
		code, short := soundCloudShortCode(link)
		if !short {
			return room.Track{}, ErrUnsupported
		}
		var err error
		if canonical, err = s.expand(ctx, code); err != nil {
			return room.Track{}, err
		}
	}
	endpoint := s.OEmbed
	if endpoint == "" {
		endpoint = soundCloudOEmbed
	}
	q := url.Values{"format": {"json"}, oembedURL: {canonical}}
	var meta struct {
		Title     string `json:"title"`
		Author    string `json:"author_name"`
		Thumbnail string `json:"thumbnail_url"`
		HTML      string `json:"html"`
	}
	if err := getJSON(ctx, s.Client, endpoint+"?"+q.Encode(), &meta); err != nil {
		if errors.Is(err, ErrUnavailable) {
			return room.Track{}, err
		}
		return room.Track{}, fmt.Errorf("soundcloud: %w", err)
	}
	// The widget plays by numeric track ID, which oEmbed carries only inside
	// the iframe markup. A playlist or a user page has no track ID there.
	m := soundCloudTrack.FindStringSubmatch(meta.HTML)
	if m == nil || strings.TrimSpace(meta.Title) == "" {
		return room.Track{}, ErrUnavailable
	}
	author := strings.TrimSpace(meta.Author)
	title := strings.TrimSpace(meta.Title)
	if author != "" {
		title = strings.TrimSpace(strings.TrimSuffix(title, " by "+author))
	}
	return room.Track{
		Provider:   SoundCloudName,
		ProviderID: m[1],
		Title:      clip(title, 200),
		Artist:     clip(author, 200),
		ArtworkURL: safeImage(meta.Thumbnail, "sndcdn.com"),
	}, nil
}

// expand follows one redirect of a share link and accepts it only when it
// lands on a track page.
func (s SoundCloud) expand(ctx context.Context, code string) (string, error) {
	base := s.Short
	if base == "" {
		base = soundCloudShort
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+code, nil)
	if err != nil {
		return "", fmt.Errorf("soundcloud: %w", err)
	}
	client := *httpClient(s.Client)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("soundcloud: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 300 || resp.StatusCode > 399 {
		return "", ErrUnavailable
	}
	canonical, ok := SoundCloudTrackURL(resp.Header.Get("Location"))
	if !ok {
		return "", ErrUnsupported
	}
	return canonical, nil
}
