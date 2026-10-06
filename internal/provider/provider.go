package provider

import (
	"context"
	"ghostview/internal/httputil"
	"ghostview/internal/model"
)

type SocialProvider interface {
	Platform() model.Platform
	Capabilities() model.ProviderCapabilities
	Status() string
	Search(context.Context, string) ([]model.ProfileSummary, error)
	GetProfile(context.Context, string) (*model.Profile, error)
	GetPosts(context.Context, string, string) (*model.MediaPage, error)
	GetStories(context.Context, string) ([]model.MediaItem, error)
	GetHighlights(context.Context, string) ([]model.Highlight, error)
	ResolveDownload(context.Context, string) (*model.DownloadResource, error)
}

// Unavailable deliberately exposes no platform functionality until a permitted integration is verified.
type Unavailable struct{ Name model.Platform }

func (p Unavailable) Platform() model.Platform               { return p.Name }
func (Unavailable) Capabilities() model.ProviderCapabilities { return model.ProviderCapabilities{} }
func (Unavailable) Status() string                           { return "UNAVAILABLE" }
func (Unavailable) Search(context.Context, string) ([]model.ProfileSummary, error) {
	return nil, httputil.Unavailable
}
func (Unavailable) GetProfile(context.Context, string) (*model.Profile, error) {
	return nil, httputil.Unavailable
}
func (Unavailable) GetPosts(context.Context, string, string) (*model.MediaPage, error) {
	return nil, httputil.Unavailable
}
func (Unavailable) GetStories(context.Context, string) ([]model.MediaItem, error) {
	return nil, httputil.Unavailable
}
func (Unavailable) GetHighlights(context.Context, string) ([]model.Highlight, error) {
	return nil, httputil.Unavailable
}
func (Unavailable) ResolveDownload(context.Context, string) (*model.DownloadResource, error) {
	return nil, httputil.Unavailable
}
