package mock_test

import (
	"context"
	"errors"
	"ghostview/internal/httputil"
	"ghostview/internal/model"
	"ghostview/internal/provider/mock"
	"testing"
)

func TestMockProviderContract(t *testing.T) {
	for _, platform := range []model.Platform{model.TikTok, model.Instagram, model.Facebook} {
		t.Run(string(platform), func(t *testing.T) {
			p := mock.New(platform)
			ctx := context.Background()
			if p.Status() != "MOCK" || p.Platform() != platform || p.Capabilities() != (model.ProviderCapabilities{Search: true, Profile: true, Posts: true, Stories: true, Highlights: true, Downloads: true}) {
				t.Fatal("incorrect capability contract")
			}
			for _, tc := range []struct {
				query string
				count int
			}{{"alex.morgan", 1}, {"alex", 2}, {"Alex Morgan", 2}, {"nobody", 0}} {
				result, err := p.Search(ctx, tc.query)
				if err != nil || len(result) != tc.count {
					t.Fatalf("search %q: %v %v", tc.query, result, err)
				}
			}
			profile, err := p.GetProfile(ctx, "alex.morgan")
			if err != nil || profile.AccessStatus != model.AccessPublic || profile.FollowerCount == nil {
				t.Fatalf("public profile: %v %v", profile, err)
			}
			private, err := p.GetProfile(ctx, "private.user")
			if err != nil || private.AccessStatus != model.AccessPrivate || private.PostCount != nil {
				t.Fatalf("private profile: %v %v", private, err)
			}
			if _, err := p.GetProfile(ctx, "missing"); !errors.Is(err, httputil.NotFound) {
				t.Fatalf("missing: %v", err)
			}
			page, err := p.GetPosts(ctx, "alex.morgan", "")
			if err != nil || len(page.Items) != 4 || page.NextCursor != "4" {
				t.Fatalf("page one: %v %v", page, err)
			}
			last, err := p.GetPosts(ctx, "alex.morgan", page.NextCursor)
			if err != nil || len(last.Items) != 4 || last.NextCursor != "" || last.Items[0].ID == page.Items[0].ID {
				t.Fatalf("page two: %v %v", last, err)
			}
			for _, cursor := range []string{"-1", "1", "8", "bad"} {
				if _, err := p.GetPosts(ctx, "alex.morgan", cursor); !errors.Is(err, httputil.Invalid) {
					t.Fatalf("bad cursor %s: %v", cursor, err)
				}
			}
			stories, err := p.GetStories(ctx, "alex.morgan")
			if err != nil || len(stories) != 2 || stories[0].Duration != 0 || stories[1].Duration <= 0 {
				t.Fatalf("story image/video: %v %v", stories, err)
			}
			highlights, err := p.GetHighlights(ctx, "alex.morgan")
			if err != nil || len(highlights) != 1 || len(highlights[0].Items) != 2 {
				t.Fatalf("highlights: %v %v", highlights, err)
			}
			for _, item := range append(append(page.Items, last.Items...), stories...) {
				resource, err := p.ResolveDownload(ctx, item.ID)
				if item.Downloadable {
					if err != nil || resource == nil || len(resource.Fixture) == 0 {
						t.Errorf("advertised download %s is unavailable", item.ID)
					}
				} else if err == nil {
					t.Errorf("unadvertised download %s resolves", item.ID)
				}
			}
			if _, err := p.ResolveDownload(ctx, "private-user-media"); !errors.Is(err, httputil.DownloadUnavailable) {
				t.Fatal("unknown/private ID resolved")
			}
			_, a := p.GetPosts(ctx, "private.user", "")
			_, b := p.GetStories(ctx, "private.user")
			_, c := p.GetHighlights(ctx, "private.user")
			for _, err := range []error{a, b, c} {
				if !errors.Is(err, httputil.Private) {
					t.Fatalf("private media: %v", err)
				}
			}
			ctx, cancel := context.WithCancel(ctx)
			cancel()
			if _, err := p.GetProfile(ctx, "alex.morgan"); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled request: %v", err)
			}
		})
	}
}
