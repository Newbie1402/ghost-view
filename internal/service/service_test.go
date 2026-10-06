package service

import (
	"context"
	"errors"
	"ghostview/internal/cache"
	"ghostview/internal/httputil"
	"ghostview/internal/model"
	"ghostview/internal/provider"
	"ghostview/internal/provider/mock"
	"testing"
	"time"
)

func demoService() *Service {
	return New([]provider.SocialProvider{mock.New(model.Instagram), mock.New(model.TikTok), mock.New(model.Facebook)}, cache.NewMemory(100), 10*time.Millisecond)
}

func TestServiceProfileAccessAndFailures(t *testing.T) {
	s := demoService()
	ctx := context.Background()
	p, err := s.Profile(ctx, model.Instagram, "alex.morgan")
	if err != nil || p.AccessStatus != model.AccessPublic {
		t.Fatalf("public %v %v", p, err)
	}
	p, err = s.Profile(ctx, model.Instagram, "private.user")
	if err != nil || p.AccessStatus != model.AccessPrivate || p.Bio != "" || p.FollowerCount != nil || p.PostCount != nil || p.Message != httputil.Private.Message {
		t.Fatalf("private fields exposed %v %v", p, err)
	}
	for _, tc := range []struct {
		name string
		want error
	}{{"missing", httputil.NotFound}, {"unavailable", httputil.Unavailable}, {"timeout", httputil.Timeout}, {"rate.limited", httputil.RateLimited}, {"bad/name", httputil.Invalid}} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Profile(ctx, model.Instagram, tc.name)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Profile(ctx, model.Instagram, "alex.morgan"); !errors.Is(err, httputil.Timeout) {
		t.Fatalf("cached canceled request %v", err)
	}
}

func TestServiceSearchAndMedia(t *testing.T) {
	s := demoService()
	ctx := context.Background()
	for _, tc := range []struct {
		query string
		count int
	}{{"@alex.morgan", 1}, {"Alex Morgan", 2}, {"alex", 2}, {"missing", 0}} {
		r, err := s.Search(ctx, model.Instagram, tc.query)
		if err != nil || len(r.Profiles) != tc.count {
			t.Fatalf("search %s: %v %v", tc.query, r, err)
		}
	}
	r, err := s.Search(ctx, model.Instagram, "https://www.tiktok.com/@alex.morgan")
	if err != nil || r.Platform != model.TikTok || r.Profiles[0].Platform != model.TikTok {
		t.Fatal("URL platform did not override selection")
	}
	p, err := s.Posts(ctx, model.Instagram, "alex.morgan", "")
	if err != nil || len(p.Items) != 4 || p.NextCursor != "4" {
		t.Fatalf("posts %v %v", p, err)
	}
	p, err = s.Posts(ctx, model.Instagram, "alex.morgan", "4")
	if err != nil || p.NextCursor != "" {
		t.Fatalf("pagination %v %v", p, err)
	}
	if _, err := s.Posts(ctx, model.Instagram, "alex.morgan", "\r\n"); !errors.Is(err, httputil.Invalid) {
		t.Fatal("unsafe cursor accepted")
	}
	stories, err := s.Stories(ctx, model.Instagram, "alex.morgan")
	if err != nil || len(stories) != 2 {
		t.Fatalf("stories %v %v", stories, err)
	}
	highlights, err := s.Highlights(ctx, model.Instagram, "alex.morgan")
	if err != nil || len(highlights) != 1 {
		t.Fatalf("highlights %v %v", highlights, err)
	}
	_, a := s.Posts(ctx, model.Instagram, "private.user", "")
	_, b := s.Stories(ctx, model.Instagram, "private.user")
	_, c := s.Highlights(ctx, model.Instagram, "private.user")
	for _, err := range []error{a, b, c} {
		if !errors.Is(err, httputil.Private) {
			t.Fatalf("private content guard: %v", err)
		}
	}
	resource, err := s.Download(ctx, model.Instagram, "post-1")
	if err != nil || len(resource.Fixture) == 0 {
		t.Fatalf("download %v %v", resource, err)
	}
	for _, id := range []string{"post-3", "story-2", "unknown", "private-user-media"} {
		if _, err := s.Download(ctx, model.Instagram, id); !errors.Is(err, httputil.DownloadUnavailable) {
			t.Fatalf("unavailable download %s %v", id, err)
		}
	}
	if _, err := s.Download(ctx, model.Instagram, "https://evil.example/media"); !errors.Is(err, httputil.Invalid) {
		t.Fatal("URL accepted as media ID")
	}
}

type restrictedProvider struct {
	provider.SocialProvider
	calls    int
	access   model.AccessStatus
	resource *model.DownloadResource
	failure  error
}

func (p *restrictedProvider) Capabilities() model.ProviderCapabilities {
	return model.ProviderCapabilities{Search: true, Profile: true, Downloads: true}
}
func (p *restrictedProvider) GetProfile(ctx context.Context, u string) (*model.Profile, error) {
	p.calls++
	if p.failure != nil {
		return nil, p.failure
	}
	if p.access != "" {
		return &model.Profile{Platform: p.Platform(), Username: u, AccessStatus: p.access}, nil
	}
	return p.SocialProvider.GetProfile(ctx, u)
}
func (p *restrictedProvider) ResolveDownload(context.Context, string) (*model.DownloadResource, error) {
	return p.resource, nil
}

func TestServiceUnsupportedUnavailableAndCache(t *testing.T) {
	p := &restrictedProvider{SocialProvider: mock.New(model.Instagram)}
	s := New([]provider.SocialProvider{p}, cache.NewMemory(10), time.Second)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := s.Profile(ctx, model.Instagram, "alex.morgan"); err != nil {
			t.Fatal(err)
		}
	}
	if p.calls != 1 {
		t.Fatal("profile cache missed")
	}
	_, a := s.Posts(ctx, model.Instagram, "alex.morgan", "")
	_, b := s.Stories(ctx, model.Instagram, "alex.morgan")
	_, c := s.Highlights(ctx, model.Instagram, "alex.morgan")
	for _, err := range []error{a, b, c} {
		if !errors.Is(err, httputil.Unsupported) {
			t.Fatalf("unsupported %v", err)
		}
	}
	if _, err := s.Profile(ctx, model.TikTok, "alex.morgan"); !errors.Is(err, httputil.Unavailable) {
		t.Fatalf("unconfigured provider %v", err)
	}
	live := New([]provider.SocialProvider{provider.Unavailable{Name: model.Instagram}}, nil, time.Second)
	if _, err := live.Search(ctx, model.Instagram, "alex"); !errors.Is(err, httputil.Unavailable) {
		t.Fatalf("live provider %v", err)
	}
	p.failure = errors.New("https://password:secret@internal-host.example?token=credential")
	if _, err := s.Profile(ctx, model.Instagram, "uncached"); !errors.Is(err, httputil.Unavailable) {
		t.Fatalf("provider details leaked: %v", err)
	}
}

func TestServiceRejectsPrivateDownloadAndUnavailableProfile(t *testing.T) {
	ctx := context.Background()
	p := &restrictedProvider{SocialProvider: mock.New(model.Instagram), access: model.AccessPrivate, resource: &model.DownloadResource{Username: "private.user", Fixture: []byte("hidden")}}
	s := New([]provider.SocialProvider{p}, nil, time.Second)
	if _, err := s.Download(ctx, model.Instagram, "private-media"); !errors.Is(err, httputil.Private) {
		t.Fatalf("private resolved media exposed: %v", err)
	}
	p.access = model.AccessUnavailable
	if _, err := s.Profile(ctx, model.Instagram, "alex.morgan"); !errors.Is(err, httputil.Unavailable) {
		t.Fatalf("unavailable access accepted: %v", err)
	}
}

type provenanceProvider struct {
	provider.SocialProvider
	status        string
	claimedSource string
}

func (p provenanceProvider) Status() string { return p.status }
func (p provenanceProvider) GetProfile(ctx context.Context, u string) (*model.Profile, error) {
	profile, err := p.SocialProvider.GetProfile(ctx, u)
	if profile != nil {
		profile.DataSource = p.claimedSource
	}
	return profile, err
}
func (p provenanceProvider) Search(ctx context.Context, q string) ([]model.ProfileSummary, error) {
	profiles, err := p.SocialProvider.Search(ctx, q)
	for i := range profiles {
		profiles[i].DataSource = p.claimedSource
	}
	return profiles, err
}
func TestServiceDataSourceComesFromProviderStatus(t *testing.T) {
	for _, tc := range []struct{ status, claim, want string }{{"MOCK", "REAL", "MOCK"}, {"EXPERIMENTAL", "MOCK", "REAL"}, {"VERIFIED", "MOCK", "REAL"}} {
		t.Run(tc.status, func(t *testing.T) {
			p := provenanceProvider{SocialProvider: mock.New(model.Instagram), status: tc.status, claimedSource: tc.claim}
			s := New([]provider.SocialProvider{p}, cache.NewMemory(10), time.Second)
			for _, name := range []string{"alex.morgan", "private.user"} {
				profile, err := s.Profile(context.Background(), model.Instagram, name)
				if err != nil || profile.DataSource != tc.want {
					t.Fatalf("profile source %v %v; want %s", profile, err, tc.want)
				}
			}
			result, err := s.Search(context.Background(), model.Instagram, "alex")
			if err != nil || result.DataSource != tc.want {
				t.Fatalf("search source %v %v", result, err)
			}
			for _, summary := range result.Profiles {
				if summary.DataSource != tc.want {
					t.Fatalf("summary trusts claimed provenance: %v", summary)
				}
			}
			info := s.Providers()
			if len(info) != 1 || info[0].DataSource != tc.want {
				t.Fatalf("provider info source %v", info)
			}
		})
	}
}
