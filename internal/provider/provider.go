// Package provider turns what a member pastes or searches into a playable
// track on a music provider.
package provider

import (
	"context"
	"errors"

	"github.com/urmzd/jamalong/internal/room"
)

// Errors a Resolver returns. Their text is safe to show to members.
var (
	ErrUnsupported = errors.New("that link is not from a supported provider")
	ErrUnavailable = errors.New("that track cannot be played here")
)

// Resolver turns a link into a track.
type Resolver interface {
	Resolve(ctx context.Context, link string) (room.Track, error)
}
