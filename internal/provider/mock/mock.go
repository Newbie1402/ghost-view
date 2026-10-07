package mock

import (
	"context"
	"embed"
	"fmt"
	"ghostview/internal/httputil"
	"ghostview/internal/model"
	"strconv"
	"strings"
	"time"
)

//go:embed fixtures/*.svg
var fixtureFiles embed.FS

type Provider struct{ platform model.Platform }

func New(p model.Platform) *Provider         { return &Provider{platform: p} }
func (p *Provider) Platform() model.Platform { return p.platform }
func (*Provider) Status() string             { return "MOCK" }
func (*Provider) Capabilities() model.ProviderCapabilities {
	return model.ProviderCapabilities{Search: true, Profile: true, Posts: true, Stories: true, Highlights: true, Downloads: true}
}
func check(ctx context.Context, username string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch username {
	case "unavailable":
		return httputil.Unavailable
	case "rate.limited":
		return httputil.RateLimited
	case "timeout":
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}
func (p *Provider) Search(ctx context.Context, q string) ([]model.ProfileSummary, error) {
	if err := check(ctx, q); err != nil {
		return nil, err
	}
	out := []model.ProfileSummary{}
	for _, name := range []string{"alex.morgan", "alex.river", "private.user"} {
		if q == name || (q == "alex" && name != "private.user") || (strings.EqualFold(q, "Alex Morgan") && name != "private.user") {
			profile, _ := p.GetProfile(ctx, name)
			out = append(out, model.ProfileSummary{Platform: p.platform, Username: name, DisplayName: profile.DisplayName, AvatarURL: profile.AvatarURL, Verified: profile.Verified})
		}
	}
	return out, nil
}
func (p *Provider) GetProfile(ctx context.Context, username string) (*model.Profile, error) {
	if err := check(ctx, username); err != nil {
		return nil, err
	}
	if username != "alex.morgan" && username != "alex.river" && username != "private.user" {
		return nil, httputil.NotFound
	}
	profile := &model.Profile{Platform: p.platform, Username: username, DisplayName: "Alex Morgan", AvatarURL: "/assets/fixtures/avatar.svg", ProfileURL: profileURL(p.platform, username), AccessStatus: model.AccessPublic, Verified: username == "alex.morgan", Bio: "Finding little moments, near and far. Photography · travel · everyday life. Demo fixture."}
	if username == "private.user" {
		profile.DisplayName = "Private Demo"
		profile.AccessStatus = model.AccessPrivate
		profile.Message = httputil.Private.Message
		profile.Bio = ""
		return profile, nil
	}
	followers, following, posts := int64(24800), int64(412), int64(8)
	profile.FollowerCount = &followers
	profile.FollowingCount = &following
	profile.PostCount = &posts
	return profile, nil
}
func profileURL(p model.Platform, u string) string {
	switch p {
	case model.TikTok:
		return "https://www.tiktok.com/@" + u
	case model.Instagram:
		return "https://www.instagram.com/" + u + "/"
	default:
		return "https://www.facebook.com/" + u
	}
}
func (p *Provider) public(ctx context.Context, u string) error {
	v, e := p.GetProfile(ctx, u)
	if e != nil {
		return e
	}
	if v.AccessStatus != model.AccessPublic {
		return httputil.Private
	}
	return nil
}
func (p *Provider) GetPosts(ctx context.Context, u, cursor string) (*model.MediaPage, error) {
	if e := p.public(ctx, u); e != nil {
		return nil, e
	}
	start := 0
	if cursor != "" {
		n, e := strconv.Atoi(cursor)
		if e != nil || n < 0 || n >= 8 || n%4 != 0 {
			return nil, httputil.Invalid
		}
		start = n
	}
	items := make([]model.MediaItem, 0, 4)
	assets := []string{"coast", "city", "studio", "mountains"}
	captions := []string{"A slower kind of morning.", "A different perspective.", "Small details, good light.", "Taking the scenic route."}
	for i := start; i < start+4; i++ {
		item := model.MediaItem{ID: fmt.Sprintf("post-%d", i+1), Type: model.Image, ThumbnailURL: "/assets/fixtures/" + assets[i%4] + ".svg", MediaURL: "/assets/fixtures/" + assets[i%4] + ".svg", Width: 800, Height: 1000, CreatedAt: time.Date(2026, 10, 1-i, 9, 0, 0, 0, time.UTC), Caption: captions[i%4], Downloadable: true}
		if i == 2 || i == 6 {
			item.Type = model.Reel
			item.MediaURL = "/assets/fixtures/story.mp4"
			item.Width = 360
			item.Height = 640
			item.Duration = 3
			item.Downloadable = false
		}
		items = append(items, item)
	}
	page := &model.MediaPage{Items: items}
	if start+4 < 8 {
		page.NextCursor = strconv.Itoa(start + 4)
	}
	return page, nil
}
func (p *Provider) GetReposts(ctx context.Context, u, cursor string) (*model.MediaPage, error) {
	return nil, httputil.Unsupported
}
func (p *Provider) GetStories(ctx context.Context, u string) ([]model.MediaItem, error) {
	if e := p.public(ctx, u); e != nil {
		return nil, e
	}
	return []model.MediaItem{{ID: "story-1", Type: model.Story, ThumbnailURL: "/assets/fixtures/coast.svg", MediaURL: "/assets/fixtures/coast.svg", Width: 800, Height: 1000, CreatedAt: time.Date(2026, 10, 6, 1, 0, 0, 0, time.UTC), Caption: "By the coast. Demo story.", Downloadable: true}, {ID: "story-2", Type: model.Story, ThumbnailURL: "/assets/fixtures/mountains.svg", MediaURL: "/assets/fixtures/story.mp4", Width: 360, Height: 640, Duration: 3, CreatedAt: time.Date(2026, 10, 6, 2, 0, 0, 0, time.UTC), Caption: "A moment in motion. Demo video.", Downloadable: false}}, nil
}
func (p *Provider) GetHighlights(ctx context.Context, u string) ([]model.Highlight, error) {
	stories, e := p.GetStories(ctx, u)
	if e != nil {
		return nil, e
	}
	return []model.Highlight{{ID: "highlight-1", Title: "Little escapes", ThumbnailURL: "/assets/fixtures/coast.svg", Items: stories}}, nil
}
func (p *Provider) ResolveDownload(ctx context.Context, id string) (*model.DownloadResource, error) {
	if e := check(ctx, ""); e != nil {
		return nil, e
	}
	valid := id == "story-1"
	if strings.HasPrefix(id, "post-") {
		n, e := strconv.Atoi(strings.TrimPrefix(id, "post-"))
		valid = e == nil && n >= 1 && n <= 8 && n != 3 && n != 7
	}
	if !valid {
		return nil, httputil.DownloadUnavailable
	}
	asset := "coast"
	if strings.HasPrefix(id, "post-") {
		n, _ := strconv.Atoi(strings.TrimPrefix(id, "post-"))
		asset = []string{"coast", "city", "studio", "mountains"}[(n-1)%4]
	}
	data, err := fixtureFiles.ReadFile("fixtures/" + asset + ".svg")
	if err != nil {
		return nil, httputil.DownloadUnavailable
	}
	return &model.DownloadResource{Username: "alex.morgan", Filename: "ghostview-" + id + ".svg", ContentType: "image/svg+xml", Fixture: data}, nil
}
