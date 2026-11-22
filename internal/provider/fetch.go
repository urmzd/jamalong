package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/urmzd/jamalong/internal/room"
)

// oembedURL is the query parameter that names the page oEmbed describes.
const oembedURL = "url"

// webURL parses an http or https link.
func webURL(link string) (*url.URL, bool) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, false
	}
	return u, true
}

// maxBody caps how much of a provider response a resolver reads.
const maxBody = 1 << 20

func httpClient(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return &http.Client{Timeout: 5 * time.Second}
}

// get fetches rawURL and returns its body. A status other than 200 becomes
// ErrUnavailable, because providers answer 401, 403 and 404 for tracks that
// are private, removed or blocked from embedding.
func get(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	// Some providers serve a reduced page to clients with no user agent.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; jamalong)")
	resp, err := httpClient(client).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, ErrUnavailable
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBody))
}

// getJSON fetches rawURL and decodes its JSON body into v.
func getJSON(ctx context.Context, client *http.Client, rawURL string, v any) error {
	body, err := get(ctx, client, rawURL)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}

// safeImage returns rawURL when it is an https URL on one of the allowed
// hosts, and "" otherwise. The page's content security policy lists the same
// hosts, so an artwork URL outside them would not render anyway.
func safeImage(rawURL string, hostSuffixes ...string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	for _, suffix := range hostSuffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return u.String()
		}
	}
	return ""
}

// Chain tries each resolver in order and returns the first answer from one
// that recognizes the link.
type Chain []Resolver

// Resolve implements Resolver.
func (c Chain) Resolve(ctx context.Context, link string) (room.Track, error) {
	for _, r := range c {
		track, err := r.Resolve(ctx, link)
		if !errors.Is(err, ErrUnsupported) {
			return track, err
		}
	}
	return room.Track{}, ErrUnsupported
}
