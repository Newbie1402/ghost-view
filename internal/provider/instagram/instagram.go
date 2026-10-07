// Package instagram reads only the public JSON embedded in anonymous profile HTML.
// It never calls private APIs, logs in, sends cookies, or solves access challenges.
package instagram

import (
	"context"
	"encoding/json"
	"fmt"
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
var shortcodePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{5,25}$`)
var storyTitlePattern = regexp.MustCompile(`(?i)watch this story by`)
var storiesTitlePattern = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title\s*>`)
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
	// sessionID is the optional operator-configured Instagram session cookie
	// (the operator's own account). It is the only way Instagram serves story
	// items; it is sent solely for that operation and is never logged.
	sessionID string
}

func New() provider.SocialProvider { return NewWithSession("") }

// NewWithSession enables the story capability when the operator supplies their
// own Instagram session cookie. Anonymous behavior is unchanged when empty.
func NewWithSession(sessionID string) provider.SocialProvider {
	return &Provider{Unavailable: provider.Unavailable{Name: model.Instagram}, sessionID: sessionID, bundleCache: cache.NewMemory(32), client: &http.Client{
		Timeout:       12 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}
func (*Provider) Status() string { return "EXPERIMENTAL" }
func (p *Provider) Capabilities() model.ProviderCapabilities {
	return model.ProviderCapabilities{Search: true, Profile: true, Posts: true, Highlights: true, Downloads: true, Stories: p.sessionID != ""}
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
	if page.Profile.AccessStatus == model.AccessPublic {
		// The anonymous stories document discloses active-story presence. Story
		// media itself stays login-only, so this signal only feeds an honest
		// disclosure; items are never guessed.
		if present, known := p.activeStoryPresence(ctx, username); known && present {
			page.Profile.Message = "This account has an active public story. Instagram serves story media only after login, so it is not retrieved."
		}
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
	// Multi-image posts embed only their cover in the profile timeline; the
	// full carousel children live on the post page. Fetch them (bounded and in
	// parallel) so every image is viewable; a failed expansion falls back to
	// the cover instead of failing the whole feed.
	type expansion struct {
		index    int
		children []carouselChild
	}
	var wg sync.WaitGroup
	results := make([]expansion, len(page.Posts))
	sem := make(chan struct{}, 4)
	expandCtx, cancel := context.WithTimeout(ctx, 7*time.Second)
	defer cancel()
	for i, item := range page.Posts {
		if !page.CarouselCodes[item.ID] {
			continue
		}
		wg.Add(1)
		go func(index int, code string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var body []byte
			key := "instagram:post:" + code
			if p.bundleCache != nil {
				if b, ok := p.bundleCache.Get(key); ok {
					body = b
				}
			}
			if body == nil {
				fetched, err := p.fetchPostPage(expandCtx, code)
				if err != nil {
					return
				}
				body = fetched
				if p.bundleCache != nil {
					p.bundleCache.Set(key, body, 5*time.Minute)
				}
			}
			children := parsePostChildren(body, code)
			if len(children) < 2 {
				return
			}
			results[index] = expansion{index: index, children: children}
		}(i, item.ID)
	}
	wg.Wait()
	flattened := []model.MediaItem{}
	for i, item := range page.Posts {
		if exp := results[i]; exp.children != nil {
			for _, child := range exp.children {
				mediaType := model.Image
				if child.MediaType == 2 {
					mediaType = model.Video
				}
				flattened = append(flattened, model.MediaItem{
					ID:           fmt.Sprintf("%s-%d", item.ID, child.Index),
					Type:         mediaType,
					ThumbnailURL: child.ThumbnailURL,
					MediaURL:     child.MediaURL,
					Width:        child.Width,
					Height:       child.Height,
					Caption:      item.Caption,
					Downloadable: true,
				})
			}
			continue
		}
		flattened = append(flattened, item)
	}
	return &model.MediaPage{Items: flattened}, nil
}
// GetHighlights returns only the highlight tray (title and cover thumbnail)
// that the anonymous profile document embeds. Highlight items are served only
// to authenticated sessions and are therefore never returned here.
func (p *Provider) GetHighlights(ctx context.Context, username string) ([]model.Highlight, error) {
	page, err := p.fetch(ctx, username)
	if err != nil {
		return nil, err
	}
	if page.Profile.AccessStatus == model.AccessPrivate {
		return nil, httputil.Private
	}
	return page.Highlights, nil
}
// webAppID is the static public identifier carried by every Instagram web
// request; it is not a secret or a credential.
const webAppID = "936619743392459"

// storyItem mirrors one reels_media entry (media_type 1 = image, 2 = video).
type storyItem struct {
	ID           string `json:"id"`
	TakenAt      int64  `json:"taken_at"`
	MediaType    int    `json:"media_type"`
	ExpiringAt   int64  `json:"expiring_at"`
	OriginalW    int    `json:"original_width"`
	OriginalH    int    `json:"original_height"`
	ImageVersions struct {
		Candidates []struct {
			URL    string `json:"url"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"candidates"`
	} `json:"image_versions2"`
	VideoVersions []struct {
		URL    string `json:"url"`
		Type   int    `json:"type"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"video_versions"`
	Caption *struct {
		Text string `json:"text"`
	} `json:"caption"`
}

// GetStories returns the active public story items of one account. The
// operation requires an authenticated Instagram session, so it exists only
// when the operator configured their own session cookie; without it the
// capability stays false and no fallback is offered.
func (p *Provider) GetStories(ctx context.Context, username string) ([]model.MediaItem, error) {
	if p.sessionID == "" {
		return nil, httputil.Unsupported
	}
	if !usernamePattern.MatchString(username) {
		return nil, httputil.Invalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	coolingDown := time.Now().Before(p.cooldownUntil)
	p.mu.Unlock()
	if coolingDown {
		return nil, httputil.RateLimited
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.instagram.com/api/v1/feed/reels_media/?reel_ids="+url.QueryEscape("@"+username), nil)
	if err != nil {
		return nil, httputil.Unavailable
	}
	req.Header.Set("User-Agent", "GhostView/1.0 (+public-content verification)")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-IG-App-ID", webAppID)
	req.Header.Set("Cookie", "sessionid="+p.sessionID)
	res, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, httputil.Unavailable
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusTooManyRequests:
		until := httputil.RetryAfterTime(res.Header.Get("Retry-After"), time.Now())
		p.mu.Lock()
		if until.After(p.cooldownUntil) {
			p.cooldownUntil = until
		}
		p.mu.Unlock()
		return nil, httputil.RateLimited
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		// An expired or invalid operator session must fail closed, never silently degrade.
		return nil, httputil.Unavailable
	default:
		return nil, httputil.Unavailable
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20+1))
	if err != nil || len(body) > 8<<20 {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, httputil.Unavailable
	}
	var payload struct {
		Reels map[string]struct {
			Items []storyItem `json:"items"`
		} `json:"reels"`
		Status string `json:"status"`
	}
	if json.Unmarshal(body, &payload) != nil || (payload.Status != "" && payload.Status != "ok") {
		return nil, httputil.Unavailable
	}
	items := []model.MediaItem{}
	for _, tray := range payload.Reels {
		for _, entry := range tray.Items {
			if entry.ID == "" || entry.MediaType != 1 && entry.MediaType != 2 {
				continue
			}
			createdAt := time.Unix(entry.TakenAt, 0).UTC()
			caption := ""
			if entry.Caption != nil {
				caption = entry.Caption.Text
			}
			item := model.MediaItem{ID: entry.ID, Type: model.Story, Caption: caption, CreatedAt: createdAt, Downloadable: false}
			best, bestWidth := "", -1
			for _, candidate := range entry.ImageVersions.Candidates {
				if url := safeImageURL(candidate.URL); url != "" && candidate.Width > bestWidth {
					best, bestWidth = url, candidate.Width
				}
			}
			if best == "" {
				continue
			}
			item.ThumbnailURL = best
			item.MediaURL = best
			item.Width, item.Height = entry.OriginalW, entry.OriginalH
			if entry.MediaType == 2 {
				for _, version := range entry.VideoVersions {
					if url := safeImageURL(version.URL); url != "" {
						item.MediaURL = url
						break
					}
				}
			}
			items = append(items, item)
		}
	}
	return items, nil
}

// ResolveDownload serves one public post by shortcode, reading the media
// manifest that the anonymous post document embeds. Gated or ambiguous media
// fails closed.
func (p *Provider) ResolveDownload(ctx context.Context, shortcode string) (*model.DownloadResource, error) {
	if !shortcodePattern.MatchString(shortcode) {
		return nil, httputil.Invalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := "instagram:post:" + shortcode
	if p.bundleCache != nil {
		if b, ok := p.bundleCache.Get(key); ok {
			var resource model.DownloadResource
			if json.Unmarshal(b, &resource) == nil && resource.URL != "" {
				return &resource, nil
			}
		}
	}
	// A "-<index>" suffix selects one image/video of a multi-image post that
	// GetPosts already expanded.
	code, childIndex := shortcode, 0
	if idx := strings.LastIndex(shortcode, "-"); idx > 0 && allDigits(shortcode[idx+1:]) {
		code, childIndex = shortcode[:idx], atoi(shortcode[idx+1:])
	}
	var body []byte
	if p.bundleCache != nil {
		if b, ok := p.bundleCache.Get("instagram:post:" + code); ok {
			body = b
		}
	}
	if body == nil {
		fetched, err := p.fetchPostPage(ctx, code)
		if err != nil {
			return nil, err
		}
		body = fetched
		if p.bundleCache != nil {
			p.bundleCache.Set("instagram:post:"+code, body, 5*time.Minute)
		}
	}
	resource, err := parsePostMedia(body, code)
	if err != nil {
		return nil, err
	}
	if childIndex > 0 {
		var chosen *carouselChild
		for i := range childrenOf(body, code) {
			if childrenOf(body, code)[i].Index == childIndex {
				chosen = &childrenOf(body, code)[i]
			}
		}
		if chosen == nil || chosen.MediaURL == "" {
			return nil, httputil.DownloadUnavailable
		}
		resource.URL = chosen.MediaURL
		resource.ContentType = "image/jpeg"
		if chosen.MediaType == 2 {
			resource.ContentType = "video/mp4"
		}
		resource.Filename = "ghostview-instagram-" + shortcode
	}
	if p.bundleCache != nil {
		if b, e := json.Marshal(resource); e == nil {
			p.bundleCache.Set(key, b, time.Minute)
		}
	}
	return resource, nil
}

func childrenOf(body []byte, code string) []carouselChild { return parsePostChildren(body, code) }

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

// activeStoryPresence reports whether the anonymous /stories/<username>
// document advertises an active story in its title. Every transport failure is
// non-fatal: the signal is a disclosure aid, never a data source.
func (p *Provider) activeStoryPresence(ctx context.Context, username string) (present bool, known bool) {
	key := "instagram:storypresence:" + strings.ToLower(username)
	if p.bundleCache != nil {
		if b, ok := p.bundleCache.Get(key); ok {
			if json.Unmarshal(b, &present) == nil {
				return present, true
			}
		}
	}
	p.mu.Lock()
	coolingDown := time.Now().Before(p.cooldownUntil)
	p.mu.Unlock()
	if coolingDown {
		return false, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.instagram.com/stories/"+url.PathEscape(username)+"/", nil)
	if err != nil {
		return false, false
	}
	req.Header.Set("User-Agent", "GhostView/1.0 (+public-content verification)")
	req.Header.Set("Accept", "text/html")
	res, err := p.client.Do(req)
	if err != nil {
		return false, false
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusTooManyRequests:
		until := httputil.RetryAfterTime(res.Header.Get("Retry-After"), time.Now())
		p.mu.Lock()
		if until.After(p.cooldownUntil) {
			p.cooldownUntil = until
		}
		p.mu.Unlock()
		return false, false
	default:
		return false, false
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxHTMLBytes+1))
	if err != nil || len(body) > maxHTMLBytes {
		return false, false
	}
	if title := storiesTitlePattern.FindSubmatch(body); len(title) > 1 {
		present = storyTitlePattern.Match(title[1])
	}
	if p.bundleCache != nil {
		if b, e := json.Marshal(present); e == nil {
			p.bundleCache.Set(key, b, 2*time.Minute)
		}
	}
	return present, true
}

func (p *Provider) fetchPostPage(ctx context.Context, shortcode string) ([]byte, error) {	p.mu.Lock()
	coolingDown := time.Now().Before(p.cooldownUntil)
	p.mu.Unlock()
	if coolingDown {
		return nil, httputil.RateLimited
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.instagram.com/p/"+url.PathEscape(shortcode)+"/", nil)
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
	return body, nil
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
	Highlights      []model.Highlight
	CarouselCodes   map[string]bool
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
				ID          string `json:"id"`
				Code        string `json:"code"`
				MediaType   int    `json:"media_type"`
				ProductType string `json:"product_type"`
				DisplayURI  string `json:"display_uri"`
				Author      *struct {
					Username string `json:"username"`
				} `json:"user"`
				CarouselCount int `json:"carousel_media_count"`
				Caption *struct {
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
	highlightSources := []any{}
	for _, script := range jsonScripts.FindAllSubmatch(body, -1) {
		var data any
		if json.Unmarshal(script[1], &data) == nil {
			collectFragments(data, 0, &fragments)
			highlightSources = append(highlightSources, data)
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
	}, Posts: []model.MediaItem{}, CarouselCodes: map[string]bool{}}
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
			// The anonymous timeline can inject posts by other accounts (tagged or
			// suggested media), so every node must prove its author.
			if n.Author == nil || !strings.EqualFold(n.Author.Username, user.Username) {
				continue
			}
			// The public bootstrap carries image previews and video cover images only;
			// playable video is resolved per post at download time.
			if (n.MediaType != 1 && n.MediaType != 2 && n.MediaType != 8) || !usernamePattern.MatchString(n.Code) {
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
			if n.CarouselCount > 1 {
				page.CarouselCodes[n.Code] = true
			}
			item := model.MediaItem{ID: n.Code, ThumbnailURL: imageURL, Caption: caption, PreviewOnly: true, Downloadable: true}
			if n.MediaType == 2 {
				item.Type = model.Video
				if strings.EqualFold(n.ProductType, "clips") {
					item.Type = model.Reel
				}
			} else {
				item.Type = model.Image
				item.MediaURL = imageURL
			}
			page.Posts = append(page.Posts, item)
		}
	}
	page.Highlights = collectHighlights(user.Username, highlightSources)
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
func collectHighlights(username string, sources []any) []model.Highlight {
	highlights := []model.Highlight{}
	for _, source := range sources {
		collectHighlightEdges(source, 0, &highlights, username)
	}
	return highlights
}

// collectHighlightEdges reads highlight tray nodes (id, title, cover) that the
// anonymous profile document embeds. Nodes without a validated public cover or
// a matching owner are ignored rather than guessed.
func collectHighlightEdges(value any, depth int, out *[]model.Highlight, username string) {
	if depth > 64 || len(*out) >= 100 {
		return
	}
	switch v := value.(type) {
	case map[string]any:
		if connection, ok := v["lox_highlights_connection"].(map[string]any); ok {
			if edges, ok := connection["edges"].([]any); ok {
				for _, edge := range edges {
					wrapper, _ := edge.(map[string]any)
					if wrapper == nil {
						continue
					}
					node, _ := wrapper["node"].(map[string]any)
					if node == nil {
						continue
					}
					id, _ := node["id"].(string)
					title, _ := node["title"].(string)
					owner, _ := node["owner_username"].(string)
					cover, _ := node["cover_media_cropped_thumbnail_url"].(string)
					if id == "" || title == "" || !strings.EqualFold(owner, username) {
						continue
					}
					if thumbnail := safeImageURL(cover); thumbnail != "" {
						*out = append(*out, model.Highlight{ID: id, Title: title, ThumbnailURL: thumbnail, Items: []model.MediaItem{}})
					}
				}
			}
			return
		}
		for _, child := range v {
			collectHighlightEdges(child, depth+1, out, username)
		}
	case []any:
		for _, child := range v {
			collectHighlightEdges(child, depth+1, out, username)
		}
	}
}

// parsePostMedia extracts the one embedded media block whose code matches the
// requested shortcode and fails closed when it is gated, ambiguous, or lacks a
// safely hosted manifest.
func parsePostMedia(body []byte, shortcode string) (*model.DownloadResource, error) {
	for _, script := range jsonScripts.FindAllSubmatch(body, -1) {
		var data any
		if json.Unmarshal(script[1], &data) != nil {
			continue
		}
		var media map[string]any
		if !findPostMedia(data, 0, shortcode, &media) {
			continue
		}
		return resourceFromMedia(media, shortcode)
	}
	return nil, httputil.Unavailable
}

// carouselChild is one media entry of a multi-image/video post.
type carouselChild struct {
	Index       int
	MediaType   int
	MediaURL    string
	ThumbnailURL string
	Width       int
	Height      int
}

// parsePostChildren extracts every carousel child of the requested post from
// its anonymous post document. Children without a safely hosted manifest are
// skipped; gated posts return nothing.
func parsePostChildren(body []byte, shortcode string) []carouselChild {
	for _, script := range jsonScripts.FindAllSubmatch(body, -1) {
		var data any
		if json.Unmarshal(script[1], &data) != nil {
			continue
		}
		var media map[string]any
		if !findPostMedia(data, 0, shortcode, &media) {
			continue
		}
		if ruling, present := media["gating_ruling"]; present && ruling != nil {
			if s, isString := ruling.(string); !isString || s != "" {
				return nil
			}
		}
		gated, ok := media["if_not_gated_logged_out"].(map[string]any)
		if !ok {
			return nil
		}
		children, ok := gated["carousel_media"].([]any)
		if !ok || len(children) == 0 {
			return nil
		}
		out := []carouselChild{}
		for i, candidate := range children {
			child, _ := candidate.(map[string]any)
			if child == nil {
				continue
			}
			block, ok := child["if_not_gated_logged_out"].(map[string]any)
			if !ok {
				block = child
			}
			entry := carouselChild{Index: i + 1}
			if w, ok := block["media_type"].(float64); ok {
				entry.MediaType = int(w)
			}
			if versions, ok := block["video_versions"].([]any); ok && entry.MediaType == 2 {
				for _, version := range versions {
					version, _ := version.(map[string]any)
					raw, _ := version["url"].(string)
					if url := safeImageURL(raw); url != "" {
						entry.MediaURL = url
						break
					}
				}
			}
			if versions, ok := block["image_versions2"].(map[string]any); ok {
				if candidates, ok := versions["candidates"].([]any); ok {
					bestURL, bestWidth := "", -1
					thumbURL, thumbWidth := "", 1<<30
					for _, candidate := range candidates {
						version, _ := candidate.(map[string]any)
						raw, _ := version["url"].(string)
						width, _ := version["width"].(float64)
						if url := safeImageURL(raw); url != "" {
							if int(width) > bestWidth {
								bestURL, bestWidth = url, int(width)
							}
							if int(width) < thumbWidth && int(width) >= 320 {
								thumbURL, thumbWidth = url, int(width)
							}
						}
					}
					if entry.MediaURL == "" && bestURL != "" {
						entry.MediaURL = bestURL
					}
					if thumbURL != "" {
						entry.ThumbnailURL = thumbURL
					} else {
						entry.ThumbnailURL = bestURL
					}
				}
			}
			if w, ok := block["original_width"].(float64); ok {
				entry.Width = int(w)
			}
			if h, ok := block["original_height"].(float64); ok {
				entry.Height = int(h)
			}
			if entry.MediaURL != "" {
				out = append(out, entry)
			}
		}
		return out
	}
	return nil
}

func findPostMedia(value any, depth int, shortcode string, out *map[string]any) bool {
	if depth > 64 {
		return false
	}
	switch v := value.(type) {
	case map[string]any:
		if block, ok := v["xig_polaris_media"].(map[string]any); ok {
			code, _ := block["code"].(string)
			if code == shortcode {
				*out = block
				return true
			}
		}
		for _, child := range v {
			if findPostMedia(child, depth+1, shortcode, out) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if findPostMedia(child, depth+1, shortcode, out) {
				return true
			}
		}
	}
	return false
}

func resourceFromMedia(media map[string]any, shortcode string) (*model.DownloadResource, error) {
	// A present, non-empty gating ruling means the media is withheld from
	// anonymous viewers; treat every unexpected value as restricted.
	if ruling, present := media["gating_ruling"]; present && ruling != nil {
		if s, isString := ruling.(string); !isString || s != "" {
			return nil, httputil.Unavailable
		}
	}
	gated, ok := media["if_not_gated_logged_out"].(map[string]any)
	if !ok {
		return nil, httputil.Unavailable
	}
	user, _ := gated["user"].(map[string]any)
	username, _ := user["username"].(string)
	if !usernamePattern.MatchString(username) {
		return nil, httputil.Unavailable
	}
	resource := &model.DownloadResource{
		Username:     username,
		Filename:     "ghostview-instagram-" + shortcode,
		AllowedHosts: []string{"*.cdninstagram.com", "*.fbcdn.net"},
	}
	if versions, ok := gated["video_versions"].([]any); ok {
		for _, candidate := range versions {
			version, _ := candidate.(map[string]any)
			raw, _ := version["url"].(string)
			if url := safeImageURL(raw); url != "" {
				resource.URL = url
				resource.ContentType = "video/mp4"
				return resource, nil
			}
		}
	}
	if versions, ok := gated["image_versions2"].(map[string]any); ok {
		if candidates, ok := versions["candidates"].([]any); ok {
			bestURL, bestWidth := "", -1
			for _, candidate := range candidates {
				version, _ := candidate.(map[string]any)
				raw, _ := version["url"].(string)
				// Width is optional in real payloads; the first valid candidate wins ties.
				width, _ := version["width"].(float64)
				if url := safeImageURL(raw); url != "" && int(width) > bestWidth {
					bestURL, bestWidth = url, int(width)
				}
			}
			if bestURL != "" {
				resource.URL = bestURL
				resource.ContentType = "image/jpeg"
				return resource, nil
			}
		}
	}
	return nil, httputil.Unavailable
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
