package provider_test

import (
	"context"
	"errors"
	"ghostview/internal/httputil"
	"ghostview/internal/model"
	"ghostview/internal/provider"
	"ghostview/internal/provider/facebook"
	"ghostview/internal/provider/instagram"
	"ghostview/internal/provider/tiktok"
	"testing"
)

// This metadata contract performs no external network requests. Retrieval is
// verified separately by opt-in live provider tests.
func TestRealProviderContracts(t *testing.T) {
	for _, tc := range []struct {
		provider provider.SocialProvider
		status   string
		caps     model.ProviderCapabilities
	}{
		{tiktok.New(), "EXPERIMENTAL", model.ProviderCapabilities{Search: true, Profile: true, Stories: true, Downloads: true}},
		{instagram.New(), "EXPERIMENTAL", model.ProviderCapabilities{Search: true, Profile: true, Posts: true, Highlights: true, Downloads: true}},
		{facebook.New(), "EXPERIMENTAL", model.ProviderCapabilities{Search: true, Profile: true}},
	} {
		p := tc.provider
		t.Run(string(p.Platform()), func(t *testing.T) {
			if !p.Platform().Valid() || p.Status() != tc.status || p.Capabilities() != tc.caps {
				t.Fatalf("provider metadata: status=%s capabilities=%+v; want %s %+v", p.Status(), p.Capabilities(), tc.status, tc.caps)
			}
			want := error(httputil.Unsupported)
			if tc.status == "UNAVAILABLE" {
				want = httputil.Unavailable
			}
			ctx := context.Background()
			var failures []error
			if !tc.caps.Search {
				_, err := p.Search(ctx, "alex")
				failures = append(failures, err)
			}
			if !tc.caps.Profile {
				_, err := p.GetProfile(ctx, "alex")
				failures = append(failures, err)
			}
			if !tc.caps.Posts {
				_, err := p.GetPosts(ctx, "alex", "")
				failures = append(failures, err)
			}
			if !tc.caps.Stories {
				_, err := p.GetStories(ctx, "alex")
				failures = append(failures, err)
			}
			if !tc.caps.Highlights {
				_, err := p.GetHighlights(ctx, "alex")
				failures = append(failures, err)
			}
			if !tc.caps.Downloads {
				_, err := p.ResolveDownload(ctx, "media")
				failures = append(failures, err)
			}
			for _, err := range failures {
				if !errors.Is(err, want) {
					t.Errorf("unsupported operation: got %v want %v", err, want)
				}
			}
		})
	}
}
