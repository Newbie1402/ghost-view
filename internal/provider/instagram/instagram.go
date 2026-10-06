// Package instagram reads only the public JSON embedded in anonymous profile HTML.
// It never calls private APIs, logs in, sends cookies, or solves access challenges.
package instagram

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"ghostview/internal/cache"
	"ghostview/internal/httputil"
	"ghostview/internal/model"
	"ghostview/internal/provider"
)

const maxHTMLBytes = 3 << 20

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.]{1,30}$`)
var invisibleElements = regexp.MustCompile(`(?is)<(?:script|style)\b[^>]*>.*?</(?:script|style)>|<!--.*?-->`)
var visibleHeadings = regexp.MustCompile(`(?is)<(?:title|h[1-6])\b[^>]*>(.*?)</(?:title|h[1-6])>`)
var htmlTags = regexp.MustCompile(`<[^>]*>`)
var accessWallText = regexp.MustCompile(`(?i)\b(?:log[ -]?in|sign[ -]?in|captcha|security[ -]+(?:check|challenge)|verify (?:you are human|you're human|that you are human|your identity)|confirm (?:it.s you|your identity))\b`)
var challengeForm = regexp.MustCompile(`(?is)<form\b[^>]*\baction=["'][^"']*/(?:challenge|checkpoint|captcha)(?:/|[?"'])`)
var jsonScripts = regexp.MustCompile(`(?is)<script[^>]*\btype=["']application/json["'][^>]*>(.*?)</script>`)

// Provider has no cookie jar and follows no redirects, including redirects to login.
type Provider struct {
	provider.Unavailable
	client        *http.Client
	bundleCache   cache.Cache
	mu            sync.Mutex
	cooldownUntil time.Time
}

func New() provider.SocialProvider {
	return &Provider{Unavailable: provider.Unavailable{Name: model.Instagram}, bundleCache: cache.NewMemory(32), client: &http.Client{
		Timeout:       12 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}
func (*Provider) Status() string { return "EXPERIMENTAL" }
func (*Provider) Capabilities() model.ProviderCapabilities {
	return model.ProviderCapabilities{Search: true, Profile: true, Posts: true}
}
func (p *Provider) Search(ctx context.Context, q string) ([]model.ProfileSummary, error) {
	if !usernamePattern.MatchString(q) {
		return nil, httputil.Unsupported
	}
	profile, err := p.GetProfile(ctx, q)
	if err == httputil.NotFound {
		return []model.ProfileSummary{}, nil
	}
	if err != nil {
		return nil, err
	}
	return []model.ProfileSummary{{Platform: model.Instagram, Username: profile.Username, DisplayName: profile.DisplayName, AvatarURL: profile.AvatarURL, Verified: profile.Verified}}, nil
}
func (p *Provider) GetProfile(ctx context.Context, username string) (*model.Profile, error) {
	page, err := p.fetch(ctx, username)
	if err != nil {
		return nil, err
	}
	return page.Profile, nil
}
func (p *Provider) GetPosts(ctx context.Context, username, cursor string) (*model.MediaPage, error) {
	if cursor != "" {
		return nil, httputil.Unsupported
	}
	page, err := p.fetch(ctx, username)
	if err != nil {
		return nil, err
	}
	if page.Profile.AccessStatus == model.AccessPrivate {
		return nil, httputil.Private
	}
	if !page.TimelinePresent {
		return nil, httputil.Unavailable
	}
	return &model.MediaPage{Items: page.Posts}, nil
}
func (*Provider) GetStories(context.Context, string) ([]model.MediaItem, error) {
	return nil, httputil.Unsupported
}
func (*Provider) GetHighlights(context.Context, string) ([]model.Highlight, error) {
	return nil, httputil.Unsupported
}
func (*Provider) ResolveDownload(context.Context, string) (*model.DownloadResource, error) {
	return nil, httputil.Unsupported
}

func (p *Provider) fetch(ctx context.Context, username string) (*publicPage, error) {
	if !usernamePattern.MatchString(username) {
		return nil, httputil.Invalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.bundleCache != nil {
		if b, ok := p.bundleCache.Get(strings.ToLower(username)); ok {
			var page publicPage
			if json.Unmarshal(b, &page) == nil {
				return &page, nil
			}
		}
	}
	p.mu.Lock()
	coolingDown := time.Now().Before(p.cooldownUntil)
	p.mu.Unlock()
	if coolingDown {
		return nil, httputil.RateLimited
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.instagram.com/"+url.PathEscape(username)+"/", nil)
	if err != nil {
		return nil, httputil.Unavailable
	}
	req.Header.Set("User-Agent", "GhostView/1.0 (+public-content verification)")
	req.Header.Set("Accept", "text/html")
	res, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusNotFound:
		return nil, httputil.NotFound
	case http.StatusTooManyRequests:
		until := httputil.RetryAfterTime(res.Header.Get("Retry-After"), time.Now())
		p.mu.Lock()
		if until.After(p.cooldownUntil) {
			p.cooldownUntil = until
		}
		p.mu.Unlock()
		return nil, httputil.RateLimited
	case http.StatusOK:
	default:
		return nil, httputil.Unavailable
	}
	if !strings.HasPrefix(strings.ToLower(res.Header.Get("Content-Type")), "text/html") {
		return nil, httputil.Unavailable
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxHTMLBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxHTMLBytes {
		return nil, httputil.Unavailable
	}
	page, err := parsePublicPage(body, username)
	if err == nil && p.bundleCache != nil {
		if normalized, e := json.Marshal(page); e == nil {
			p.bundleCache.Set(strings.ToLower(username), normalized, time.Minute)
		}
	}
	return page, err
}

type publicPage struct {
	Profile         *model.Profile
	Posts           []model.MediaItem
	TimelinePresent bool
}
type userFragment struct {
	invalid     bool
	ID          string `json:"id"`
	Username    string `json:"username"`
	FullName    string `json:"full_name"`
	Biography   string `json:"biography"`
	Avatar      string `json:"profile_pic_url"`
	Private     *bool  `json:"is_private"`
	Unpublished bool   `json:"is_unpublished"`
	Verified    bool   `json:"is_verified"`
	Followers   *int64 `json:"follower_count"`
	Following   *int64 `json:"following_count"`
	PostCount   *int64 `json:"all_media_count"`
	Timeline    *struct {
		Edges []struct {
			Node struct {
				ID         string `json:"id"`
				Code       string `json:"code"`
				MediaType  int    `json:"media_type"`
				DisplayURI string `json:"display_uri"`
				Caption    *struct {
					Text string `json:"text"`
				} `json:"caption"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"polaris_ordered_timeline_connection"`
}

func parsePublicPage(body []byte, username string) (*publicPage, error) {
	// Login/challenge pages take precedence over any stale embedded profile JSON.
	visible := invisibleElements.ReplaceAll(body, nil)
	if challengeForm.Match(visible) {
		return nil, httputil.Unavailable
	}
	for _, heading := range visibleHeadings.FindAllSubmatch(visible, -1) {
		text := strings.Join(strings.Fields(html.UnescapeString(htmlTags.ReplaceAllString(string(heading[1]), " "))), " ")
		if accessWallText.MatchString(text) {
			return nil, httputil.Unavailable
		}
	}
	fragments := []userFragment{}
	for _, script := range jsonScripts.FindAllSubmatch(body, -1) {
		var data any
		if json.Unmarshal(script[1], &data) == nil {
			collectFragments(data, 0, &fragments)
		}
	}
	var user *userFragment
	for i := range fragments {
		candidate := &fragments[i]
		if strings.EqualFold(candidate.Username, username) && candidate.Private != nil && candidate.ID != "" && !candidate.invalid {
			user = candidate
			break
		}
	}
	// Open Graph metadata and HTTP 200 alone cannot establish public account access.
	if user == nil {
		return nil, httputil.Unavailable
	}
	status := model.AccessPublic
	// Inspect every matching fragment; JSON traversal order cannot override restrictions.
	for _, fragment := range fragments {
		sameUsername := strings.EqualFold(fragment.Username, username)
		sameID := fragment.ID == user.ID
		if !sameUsername && !sameID {
			continue
		}
		if fragment.invalid || fragment.Unpublished ||
			(sameUsername && fragment.ID != "" && !sameID) ||
			(sameID && fragment.Username != "" && !sameUsername) {
			return nil, httputil.Unavailable
		}
		for _, count := range []*int64{fragment.Followers, fragment.Following, fragment.PostCount} {
			if count != nil && *count < 0 {
				return nil, httputil.Unavailable
			}
		}
		if fragment.Private != nil && *fragment.Private {
			status = model.AccessPrivate
		}
	}
	page := &publicPage{Profile: &model.Profile{
		Platform: model.Instagram, Username: user.Username, DisplayName: user.FullName,
		Bio: user.Biography, AvatarURL: safeImageURL(user.Avatar), ProfileURL: "https://www.instagram.com/" + url.PathEscape(user.Username) + "/",
		Verified: user.Verified, FollowerCount: user.Followers, FollowingCount: user.Following, PostCount: user.PostCount, AccessStatus: status,
	}, Posts: []model.MediaItem{}}
	if status == model.AccessPrivate {
		page.Profile = &model.Profile{Platform: model.Instagram, Username: user.Username, DisplayName: user.FullName, AccessStatus: status, Message: httputil.Private.Message}
		return page, nil
	}
	for _, fragment := range fragments {
		if fragment.ID != user.ID || fragment.Timeline == nil {
			continue
		}
		page.TimelinePresent = true
		for _, edge := range fragment.Timeline.Edges {
			n := edge.Node
			// Public bootstrap contains image previews, not playable video or full carousels.
			if (n.MediaType != 1 && n.MediaType != 8) || !usernamePattern.MatchString(n.Code) {
				continue
			}
			imageURL := safeImageURL(n.DisplayURI)
			if imageURL == "" {
				continue
			}
			caption := ""
			if n.Caption != nil {
				caption = n.Caption.Text
			}
			page.Posts = append(page.Posts, model.MediaItem{ID: n.Code, Type: model.Image, ThumbnailURL: imageURL, MediaURL: imageURL, Caption: caption, PreviewOnly: true})
		}
	}
	return page, nil
}
func collectFragments(value any, depth int, out *[]userFragment) {
	if depth > 64 {
		return
	}
	switch v := value.(type) {
	case map[string]any:
		if user, ok := v["xig_user_by_username"].(map[string]any); ok {
			raw, err := json.Marshal(user)
			var fragment userFragment
			if err == nil {
				if json.Unmarshal(raw, &fragment) != nil {
					// Malformed matching fragments must not hide privacy or restriction signals.
					fragment = userFragment{invalid: true}
					fragment.Username, _ = user["username"].(string)
					fragment.ID, _ = user["id"].(string)
				}
				*out = append(*out, fragment)
			}
		}
		for _, child := range v {
			collectFragments(child, depth+1, out)
		}
	case []any:
		for _, child := range v {
			collectFragments(child, depth+1, out)
		}
	}
}
func safeImageURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Fragment != "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(host, ".cdninstagram.com") && !strings.HasSuffix(host, ".fbcdn.net") {
		return ""
	}
	for key := range u.Query() {
		switch strings.ToLower(key) {
		case "access_token", "token", "sessionid", "cookie", "password":
			return ""
		}
	}
	return u.String()
}
