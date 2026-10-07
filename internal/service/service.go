package service

import (
	"context"
	"encoding/json"
	"errors"
	"ghostview/internal/cache"
	"ghostview/internal/httputil"
	"ghostview/internal/model"
	"ghostview/internal/provider"
	"strings"
	"time"
)

type Service struct {
	providers map[model.Platform]provider.SocialProvider
	cache     cache.Cache
	timeout   time.Duration
}

func New(providers []provider.SocialProvider, c cache.Cache, timeout time.Duration) *Service {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	s := &Service{providers: make(map[model.Platform]provider.SocialProvider), cache: c, timeout: timeout}
	for _, p := range providers {
		s.providers[p.Platform()] = p
	}
	return s
}
func (s *Service) Providers() []model.ProviderInfo {
	out := make([]model.ProviderInfo, 0, 3)
	for _, name := range []model.Platform{model.TikTok, model.Instagram, model.Facebook} {
		if p, ok := s.providers[name]; ok {
			msg := "Deterministic demo data. No social platform is contacted."
			if p.Status() == "UNAVAILABLE" {
				msg = "Public retrieval is unavailable. No mock fallback is used."
			} else if p.Status() != "MOCK" {
				msg = "Real public data. Only declared capabilities are supported; restrictions remain protected."
			}
			out = append(out, model.ProviderInfo{DataSource: dataSource(p), Platform: name, Status: p.Status(), Capabilities: p.Capabilities(), Message: msg})
		}
	}
	return out
}
func dataSource(p provider.SocialProvider) string {
	if p.Status() == "MOCK" {
		return "MOCK"
	}
	return "REAL"
}
func (s *Service) get(p model.Platform, supported func(model.ProviderCapabilities) bool) (provider.SocialProvider, error) {
	if !p.Valid() {
		return nil, httputil.Invalid
	}
	pr, ok := s.providers[p]
	if !ok || pr.Status() == "UNAVAILABLE" {
		return nil, httputil.Unavailable
	}
	if !supported(pr.Capabilities()) {
		return nil, httputil.Unsupported
	}
	return pr, nil
}
func safeError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return httputil.Timeout
	}
	var safe *httputil.Error
	if errors.As(err, &safe) {
		for _, known := range []*httputil.Error{httputil.NotFound, httputil.Private, httputil.Unavailable, httputil.Unsupported, httputil.Invalid, httputil.Timeout, httputil.RateLimited, httputil.DownloadUnavailable} {
			if safe.Code == known.Code {
				return known
			}
		}
	}
	return httputil.Unavailable
}
func cached[T any](ctx context.Context, s *Service, key string, ttl time.Duration, fetch func(context.Context) (T, error)) (T, error) {
	var value T
	if err := ctx.Err(); err != nil {
		return value, safeError(err)
	}
	if s.cache != nil {
		if bytes, ok := s.cache.Get(key); ok && json.Unmarshal(bytes, &value) == nil {
			return value, nil
		}
	}
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	value, err := fetch(callCtx)
	if err != nil {
		return value, safeError(err)
	}
	if err = callCtx.Err(); err != nil {
		return value, safeError(err)
	}
	if s.cache != nil {
		if bytes, e := json.Marshal(value); e == nil {
			s.cache.Set(key, bytes, ttl)
		}
	}
	return value, nil
}
func (s *Service) Search(ctx context.Context, p model.Platform, q string) (*model.SearchResult, error) {
	p, q, err := Normalize(p, q)
	if err != nil {
		return nil, err
	}
	pr, err := s.get(p, func(c model.ProviderCapabilities) bool { return c.Search })
	if err != nil {
		return nil, err
	}
	profiles, err := cached(ctx, s, string(p)+":search:"+q, 2*time.Minute, func(c context.Context) ([]model.ProfileSummary, error) { return pr.Search(c, q) })
	if err != nil {
		return nil, err
	}
	if profiles == nil {
		profiles = []model.ProfileSummary{}
	}
	profiles = append([]model.ProfileSummary{}, profiles...)
	for i := range profiles {
		profiles[i].DataSource = dataSource(pr)
	}
	return &model.SearchResult{DataSource: dataSource(pr), Platform: p, Profiles: profiles}, nil
}
func (s *Service) Profile(ctx context.Context, p model.Platform, u string) (*model.Profile, error) {
	if !ValidUsername(u) {
		return nil, httputil.Invalid
	}
	u = strings.ToLower(u)
	pr, err := s.get(p, func(c model.ProviderCapabilities) bool { return c.Profile })
	if err != nil {
		return nil, err
	}
	profile, err := cached(ctx, s, string(p)+":profile:"+u, 5*time.Minute, func(c context.Context) (*model.Profile, error) { return pr.GetProfile(c, u) })
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, httputil.Unavailable
	}
	switch profile.AccessStatus {
	case model.AccessPrivate:
		profile = &model.Profile{Platform: p, Username: u, DisplayName: profile.DisplayName, AvatarURL: profile.AvatarURL, AccessStatus: model.AccessPrivate, Message: httputil.Private.Message}
	case model.AccessPublic:
	case model.AccessUnavailable:
		return nil, httputil.Unavailable
	default:
		return nil, httputil.Unavailable
	}
	copy := *profile
	copy.DataSource = dataSource(pr)
	return &copy, nil
}
func (s *Service) public(ctx context.Context, p model.Platform, u string) error {
	profile, err := s.Profile(ctx, p, u)
	if err != nil {
		return err
	}
	if profile.AccessStatus != model.AccessPublic {
		return httputil.Private
	}
	return nil
}
func (s *Service) Posts(ctx context.Context, p model.Platform, u, cursor string) (*model.MediaPage, error) {
	if len(cursor) > 128 || strings.ContainsAny(cursor, "\r\n\x00") {
		return nil, httputil.Invalid
	}
	if err := s.public(ctx, p, u); err != nil {
		return nil, err
	}
	pr, err := s.get(p, func(c model.ProviderCapabilities) bool { return c.Posts })
	if err != nil {
		return nil, err
	}
	page, err := cached(ctx, s, string(p)+":posts:"+strings.ToLower(u)+":"+cursor, 2*time.Minute, func(c context.Context) (*model.MediaPage, error) { return pr.GetPosts(c, strings.ToLower(u), cursor) })
	if err != nil {
		return nil, err
	}
	if page == nil {
		return nil, httputil.Unavailable
	}
	if page.Items == nil {
		page.Items = []model.MediaItem{}
	}
	return page, nil
}
func (s *Service) Reposts(ctx context.Context, p model.Platform, u, cursor string) (*model.MediaPage, error) {
	if len(cursor) > 128 || strings.ContainsAny(cursor, "\r\n\x00") {
		return nil, httputil.Invalid
	}
	if err := s.public(ctx, p, u); err != nil {
		return nil, err
	}
	pr, err := s.get(p, func(c model.ProviderCapabilities) bool { return c.Reposts })
	if err != nil {
		return nil, err
	}
	page, err := cached(ctx, s, string(p)+":reposts:"+strings.ToLower(u)+":"+cursor, 2*time.Minute, func(c context.Context) (*model.MediaPage, error) { return pr.GetReposts(c, strings.ToLower(u), cursor) })
	if err != nil {
		return nil, err
	}
	if page == nil {
		return nil, httputil.Unavailable
	}
	if page.Items == nil {
		page.Items = []model.MediaItem{}
	}
	return page, nil
}
func (s *Service) Stories(ctx context.Context, p model.Platform, u string) ([]model.MediaItem, error) {
	if err := s.public(ctx, p, u); err != nil {
		return nil, err
	}
	pr, err := s.get(p, func(c model.ProviderCapabilities) bool { return c.Stories })
	if err != nil {
		return nil, err
	}
	items, err := cached(ctx, s, string(p)+":stories:"+strings.ToLower(u), 45*time.Second, func(c context.Context) ([]model.MediaItem, error) { return pr.GetStories(c, strings.ToLower(u)) })
	if items == nil && err == nil {
		items = []model.MediaItem{}
	}
	return items, err
}
func (s *Service) Highlights(ctx context.Context, p model.Platform, u string) ([]model.Highlight, error) {
	if err := s.public(ctx, p, u); err != nil {
		return nil, err
	}
	pr, err := s.get(p, func(c model.ProviderCapabilities) bool { return c.Highlights })
	if err != nil {
		return nil, err
	}
	items, err := cached(ctx, s, string(p)+":highlights:"+strings.ToLower(u), 2*time.Minute, func(c context.Context) ([]model.Highlight, error) { return pr.GetHighlights(c, strings.ToLower(u)) })
	if items == nil && err == nil {
		items = []model.Highlight{}
	}
	return items, err
}
func (s *Service) Download(ctx context.Context, p model.Platform, id string) (*model.DownloadResource, error) {
	if !ValidUsername(id) {
		return nil, httputil.Invalid
	}
	pr, err := s.get(p, func(c model.ProviderCapabilities) bool { return c.Downloads })
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	resource, err := pr.ResolveDownload(callCtx, id)
	if err != nil {
		return nil, safeError(err)
	}
	if err := callCtx.Err(); err != nil {
		return nil, safeError(err)
	}
	if resource == nil || !ValidUsername(resource.Username) {
		return nil, httputil.DownloadUnavailable
	}
	if err = s.public(ctx, p, resource.Username); err != nil {
		return nil, err
	}
	if len(resource.Fixture) > 0 && pr.Status() != "MOCK" {
		return nil, httputil.DownloadUnavailable
	}
	return resource, nil
}
