package tiktok

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"ghostview/internal/cache"
	"ghostview/internal/downloader"
	"ghostview/internal/httputil"
	"ghostview/internal/model"
	"ghostview/internal/provider"
)

const maxHTMLBytes = 8 << 20
const userAgent = "GhostView/1.0 (+public-content verification)"

// Provider reads only anonymous public surfaces: the profile HTML, the
// unsigned story API, and — when the operator opts in — the feed payloads that
// a local headless Chrome fetches by running TikTok's own anonymous web
// client. It never signs private API requests, logs in, or retains cookies.
type Provider struct {
	client        *http.Client
	cache         cache.Cache
	mu            sync.Mutex
	cooldownUntil time.Time
	browser       bool
	fetcher       *BrowserFetcher
}

func New() provider.SocialProvider { return NewWithBrowser(false) }

// NewWithBrowser enables the signature-gated post/repost feeds through a local
// headless browser when the operator sets GHOSTVIEW_TT_BROWSER=1.
func NewWithBrowser(browser bool) provider.SocialProvider {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSHandshakeTimeout = 5 * time.Second
	transport.ResponseHeaderTimeout = 8 * time.Second
	transport.MaxIdleConnsPerHost = 4
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || host != "www.tiktok.com" || port != "443" {
			return nil, httputil.Unavailable
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(addresses) == 0 {
			return nil, httputil.Unavailable
		}
		for _, address := range addresses {
			if !downloader.IsPublicIP(address.IP) {
				return nil, httputil.Unavailable
			}
		}
		dialer := net.Dialer{Timeout: 5 * time.Second}
		for _, address := range addresses {
			connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
			if err == nil {
				return connection, nil
			}
		}
		return nil, httputil.Unavailable
	}
	return &Provider{client: &http.Client{Timeout: 8 * time.Second, Transport: transport, CheckRedirect: publicProfileRedirect}, cache: cache.NewMemory(200), browser: browser, fetcher: &BrowserFetcher{}}
}

func (*Provider) Platform() model.Platform { return model.TikTok }
func (p *Provider) Capabilities() model.ProviderCapabilities {
	return model.ProviderCapabilities{Search: true, Profile: true, Posts: p.browser, Reposts: p.browser, Stories: true, Downloads: true}
}
func (*Provider) Status() string { return "EXPERIMENTAL" }

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.]{0,63}$`)
var numericIDPattern = regexp.MustCompile(`^[0-9]{1,25}$`)
var bootstrapPattern = regexp.MustCompile(`(?is)<script\b[^>]*\bid\s*=\s*["']__UNIVERSAL_DATA_FOR_REHYDRATION__["'][^>]*>(.*?)</script\s*>`)
var titlePattern = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title\s*>`)

// The anonymous web story API serves active story items for public authors
// without client signatures (verified 2026-10-07); authorId is the numeric user
// id already embedded in the profile bootstrap.
const storyAPIPath = "https://www.tiktok.com/api/story/item_list/?aid=1988&authorId="

func publicProfileRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 3 || req.URL.Scheme != "https" || req.URL.Host != "www.tiktok.com" || req.URL.User != nil || req.URL.RawQuery != "" || req.URL.Fragment != "" || !strings.HasPrefix(req.URL.Path, "/@") || !usernamePattern.MatchString(strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/@"), "/")) {
		return httputil.Unavailable
	}
	return nil
}

func (p *Provider) Search(ctx context.Context, query string) ([]model.ProfileSummary, error) {
	if strings.ContainsAny(query, " \t\r\n") {
		return nil, httputil.Unsupported
	}
	profile, err := p.GetProfile(ctx, strings.TrimPrefix(query, "@"))
	if errors.Is(err, httputil.NotFound) {
		return []model.ProfileSummary{}, nil
	}
	if err != nil {
		return nil, err
	}
	return []model.ProfileSummary{{Platform: model.TikTok, Username: profile.Username, DisplayName: profile.DisplayName, AvatarURL: profile.AvatarURL, Verified: profile.Verified}}, nil
}

func (p *Provider) GetProfile(ctx context.Context, username string) (*model.Profile, error) {
	record, err := p.fetchUserRecord(ctx, username)
	if err != nil {
		return nil, err
	}
	return record.Profile, nil
}

// userRecord pairs the parsed public profile with the numeric author id that
// the story API requires. Only PUBLIC records are cached.
type userRecord struct {
	Profile  *model.Profile `json:"profile"`
	AuthorID string         `json:"authorId"`
}

func (p *Provider) fetchUserRecord(ctx context.Context, username string) (*userRecord, error) {
	if !usernamePattern.MatchString(username) {
		return nil, httputil.Invalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := "tiktok:user:" + strings.ToLower(username)
	if p.cache != nil {
		if data, ok := p.cache.Get(key); ok {
			var record userRecord
			if json.Unmarshal(data, &record) == nil && record.Profile != nil && record.Profile.AccessStatus == model.AccessPublic {
				return &record, nil
			}
		}
	}
	p.mu.Lock()
	limited := time.Now().Before(p.cooldownUntil)
	p.mu.Unlock()
	if limited {
		return nil, httputil.RateLimited
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.tiktok.com/@"+username, nil)
	if err != nil {
		return nil, httputil.Invalid
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/json")
	response, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, context.DeadlineExceeded
		}
		return nil, httputil.Unavailable
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusTooManyRequests:
		p.mu.Lock()
		until := httputil.RetryAfterTime(response.Header.Get("Retry-After"), time.Now())
		if until.After(p.cooldownUntil) {
			p.cooldownUntil = until
		}
		p.mu.Unlock()
		return nil, httputil.RateLimited
	case http.StatusNotFound:
		return nil, httputil.NotFound
	case http.StatusOK:
	default:
		return nil, httputil.Unavailable
	}
	if response.ContentLength > maxHTMLBytes {
		return nil, httputil.Unavailable
	}
	contentType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || contentType != "text/html" {
		return nil, httputil.Unavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxHTMLBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, httputil.Unavailable
	}
	if len(body) > maxHTMLBytes {
		return nil, httputil.Unavailable
	}
	profile, authorID, err := parseProfile(body, username)
	if err != nil {
		return nil, err
	}
	record := &userRecord{Profile: profile, AuthorID: authorID}
	if err == nil && profile.AccessStatus == model.AccessPublic && p.cache != nil {
		if data, encodeErr := json.Marshal(record); encodeErr == nil {
			p.cache.Set(key, data, 30*time.Second)
		}
	}
	return record, nil
}

type publicUser struct {
	ID             string `json:"id"`
	UniqueID       string `json:"uniqueId"`
	Nickname       string `json:"nickname"`
	Signature      string `json:"signature"`
	AvatarThumb    string `json:"avatarThumb"`
	AvatarMedium   string `json:"avatarMedium"`
	AvatarLarger   string `json:"avatarLarger"`
	Verified       bool   `json:"verified"`
	PrivateAccount *bool  `json:"privateAccount"`
}
type publicStats struct {
	FollowerCount  *int64 `json:"followerCount"`
	FollowingCount *int64 `json:"followingCount"`
	VideoCount     *int64 `json:"videoCount"`
}
type publicDetail struct {
	StatusCode *int `json:"statusCode"`
	UserInfo   struct {
		User  publicUser  `json:"user"`
		Stats publicStats `json:"stats"`
	} `json:"userInfo"`
}

func parseProfile(html []byte, username string) (*model.Profile, string, error) {
	// A visible challenge page fails closed; a bundled JavaScript identifier named
	// "captcha" is not evidence that the public document is restricted.
	if title := titlePattern.FindSubmatch(html); len(title) > 1 {
		text := strings.ToLower(string(title[1]))
		for _, marker := range []string{"captcha", "verify you", "security check", "access denied", "log in", "login"} {
			if strings.Contains(text, marker) {
				return nil, "", httputil.Unavailable
			}
		}
	}
	matches := bootstrapPattern.FindAllSubmatch(html, 2)
	if len(matches) != 1 {
		return nil, "", httputil.Unavailable
	}
	var bootstrap struct {
		Scope map[string]json.RawMessage `json:"__DEFAULT_SCOPE__"`
	}
	if json.Unmarshal(matches[0][1], &bootstrap) != nil {
		return nil, "", httputil.Unavailable
	}
	var detail publicDetail
	if json.Unmarshal(bootstrap.Scope["webapp.user-detail"], &detail) != nil || detail.StatusCode == nil || *detail.StatusCode != 0 || detail.UserInfo.User.PrivateAccount == nil {
		return nil, "", httputil.Unavailable
	}
	user := detail.UserInfo.User
	if !strings.EqualFold(user.UniqueID, username) || !usernamePattern.MatchString(user.UniqueID) {
		return nil, "", httputil.Unavailable
	}
	profile := &model.Profile{Platform: model.TikTok, Username: user.UniqueID, DisplayName: user.Nickname, ProfileURL: "https://www.tiktok.com/@" + user.UniqueID, AccessStatus: model.AccessPublic}
	if *user.PrivateAccount {
		profile.AccessStatus = model.AccessPrivate
		profile.Message = httputil.Private.Message
		return profile, "", nil
	}
	stats := detail.UserInfo.Stats
	for _, count := range []*int64{stats.FollowerCount, stats.FollowingCount, stats.VideoCount} {
		if count != nil && *count < 0 {
			return nil, "", httputil.Unavailable
		}
	}
	profile.Bio = user.Signature
	profile.Verified = user.Verified
	profile.FollowerCount = stats.FollowerCount
	profile.FollowingCount = stats.FollowingCount
	profile.PostCount = stats.VideoCount
	for _, candidate := range []string{user.AvatarLarger, user.AvatarMedium, user.AvatarThumb} {
		if validMediaURL(candidate) {
			profile.AvatarURL = candidate
			break
		}
	}
	if !numericIDPattern.MatchString(user.ID) {
		return nil, "", httputil.Unavailable
	}
	return profile, user.ID, nil
}

// validMediaURL accepts HTTPS TikTok/TikTokCDN media links without credential
// query parameters. It guards avatars, story covers, and playable story media
// that the frontend loads directly.
func validMediaURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Fragment != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	permitted := false
	for _, domain := range []string{"tiktok.com", "tiktokcdn.com", "tiktokcdn-us.com", "tiktokcdn-eu.com"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			permitted = true
		}
	}
	if !permitted {
		return false
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	for key := range query {
		switch strings.ToLower(key) {
		case "access_token", "token", "session", "sessionid", "cookie", "authorization", "auth":
			return false
		}
	}
	return true
}

// feedPage mirrors the post/repost feed payloads the browser harvests.
type feedPage struct {
	ItemList []storyItem `json:"itemList"`
	HasMore  bool        `json:"hasMore"`
	Cursor   json.Number `json:"cursor"`
}

// GetPosts returns the first page of the author's public videos. TikTok signs
// this feed, so it requires the opt-in headless browser; without it the
// capability is off and nothing is fabricated.
func (p *Provider) GetPosts(ctx context.Context, username, cursor string) (*model.MediaPage, error) {
	if cursor != "" {
		return nil, httputil.Unsupported
	}
	return p.fetchFeed(ctx, username, false)
}

// GetReposts returns the first page of the author's public reposts. The items
// keep their original authors, so no per-item identity check is applied.
func (p *Provider) GetReposts(ctx context.Context, username, cursor string) (*model.MediaPage, error) {
	if cursor != "" {
		return nil, httputil.Unsupported
	}
	return p.fetchFeed(ctx, username, true)
}

func (p *Provider) fetchFeed(ctx context.Context, username string, repost bool) (*model.MediaPage, error) {
	if !p.browser {
		return nil, httputil.Unsupported
	}
	if !usernamePattern.MatchString(username) {
		return nil, httputil.Invalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	record, err := p.fetchUserRecord(ctx, username)
	if err != nil {
		return nil, err
	}
	if record.Profile.AccessStatus != model.AccessPublic {
		return nil, httputil.Private
	}
	body, err := p.fetcher.fetchFeed(ctx, username)
	if err != nil {
		return nil, err
	}
	raw := body.posts
	if repost {
		raw = body.reposts
	}
	if raw == nil {
		return nil, httputil.Unavailable
	}
	var page feedPage
	if json.Unmarshal(raw, &page) != nil {
		return nil, httputil.Unavailable
	}
	items := []model.MediaItem{}
	for _, item := range page.ItemList {
		if item.PrivateItem || item.IsAd || !storyIDPattern.MatchString(item.ID) || item.CreateTime <= 0 {
			continue
		}
		// Own posts must prove their author; reposts intentionally keep the
		// original creator.
		if !repost && item.Author.UniqueID != "" && !strings.EqualFold(item.Author.UniqueID, record.Profile.Username) {
			continue
		}
		mediaURL := ""
		if validMediaURL(item.Video.PlayAddr) {
			mediaURL = item.Video.PlayAddr
		}
		cover := ""
		for _, candidate := range []string{item.Video.Cover, item.Video.DynamicCover} {
			if validMediaURL(candidate) {
				cover = candidate
				break
			}
		}
		if mediaURL == "" && cover == "" {
			continue
		}
		downloadURL, contentType := cover, "image/jpeg"
		if mediaURL != "" {
			downloadURL, contentType = mediaURL, "video/mp4"
		}
		if p.cache != nil {
			if data, encodeErr := json.Marshal(storyRecord{Username: record.Profile.Username, URL: downloadURL, ContentType: contentType}); encodeErr == nil {
				p.cache.Set("tiktok:story:"+item.ID, data, 30*time.Minute)
			}
		}
		items = append(items, model.MediaItem{
			ID:           item.ID,
			Type:         model.Video,
			ThumbnailURL: cover,
			MediaURL:     mediaURL,
			Width:        int(item.Video.Width),
			Height:       int(item.Video.Height),
			Duration:     float64(item.Video.Duration),
			CreatedAt:    time.Unix(item.CreateTime, 0).UTC(),
			Caption:      item.Desc,
			Downloadable: true,
		})
	}
	return &model.MediaPage{Items: items}, nil
}

// storyResponse mirrors the anonymous web story payload. A non-zero statusCode
// or an unidentifiable item fails closed instead of being rendered.
type storyResponse struct {
	StatusCode *int64      `json:"statusCode"`
	TotalCount string      `json:"TotalCount"`
	ItemList   []storyItem `json:"itemList"`
}

type storyItem struct {
	ID          string `json:"id"`
	CreateTime  int64  `json:"createTime"`
	Desc        string `json:"desc"`
	PrivateItem bool   `json:"privateItem"`
	IsAd        bool   `json:"isAd"`
	Author      struct {
		UniqueID string `json:"uniqueId"`
	} `json:"author"`
	Video struct {
		PlayAddr     string `json:"playAddr"`
		Cover        string `json:"cover"`
		DynamicCover string `json:"dynamicCover"`
		Duration     int64  `json:"duration"`
		Width        int64  `json:"width"`
		Height       int64  `json:"height"`
	} `json:"video"`
}

var storyIDPattern = regexp.MustCompile(`^[0-9]{10,25}$`)

// GetStories returns the active public stories of one author. TikTok serves
// this collection to anonymous clients (unlike the signature-gated post feed);
// items are attributed to the requested username and anything ambiguous is
// skipped. Story media plays directly from the CDN; downloads are not offered.
func (p *Provider) GetStories(ctx context.Context, username string) ([]model.MediaItem, error) {
	if !usernamePattern.MatchString(username) {
		return nil, httputil.Invalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	record, err := p.fetchUserRecord(ctx, username)
	if err != nil {
		return nil, err
	}
	if record.Profile.AccessStatus != model.AccessPublic {
		return nil, httputil.Private
	}
	if record.AuthorID == "" || !numericIDPattern.MatchString(record.AuthorID) {
		return nil, httputil.Unavailable
	}
	p.mu.Lock()
	limited := time.Now().Before(p.cooldownUntil)
	p.mu.Unlock()
	if limited {
		return nil, httputil.RateLimited
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, storyAPIPath+url.QueryEscape(record.AuthorID)+"&count=20&cursor=0&device_platform=web_pc", nil)
	if err != nil {
		return nil, httputil.Invalid
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	response, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, httputil.Unavailable
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusTooManyRequests:
		p.mu.Lock()
		until := httputil.RetryAfterTime(response.Header.Get("Retry-After"), time.Now())
		if until.After(p.cooldownUntil) {
			p.cooldownUntil = until
		}
		p.mu.Unlock()
		return nil, httputil.RateLimited
	case http.StatusOK:
	default:
		return nil, httputil.Unavailable
	}
	if !strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "application/json") {
		return nil, httputil.Unavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
	if err != nil || len(body) > 4<<20 {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, httputil.Unavailable
	}
	var payload storyResponse
	if json.Unmarshal(body, &payload) != nil || payload.StatusCode == nil || *payload.StatusCode != 0 {
		return nil, httputil.Unavailable
	}
	items := []model.MediaItem{}
	for _, item := range payload.ItemList {
		if item.PrivateItem || item.IsAd || item.Author.UniqueID == "" || !strings.EqualFold(item.Author.UniqueID, record.Profile.Username) {
			continue
		}
		if !storyIDPattern.MatchString(item.ID) || item.CreateTime <= 0 {
			continue
		}
		mediaURL := ""
		if validMediaURL(item.Video.PlayAddr) {
			mediaURL = item.Video.PlayAddr
		}
		cover := ""
		for _, candidate := range []string{item.Video.Cover, item.Video.DynamicCover} {
			if validMediaURL(candidate) {
				cover = candidate
				break
			}
		}
		if mediaURL == "" && cover == "" {
			continue
		}
		downloadURL, contentType := cover, "image/jpeg"
		if mediaURL != "" {
			downloadURL, contentType = mediaURL, "video/mp4"
		}
		if p.cache != nil {
			// Signed playAddr URLs are referer-bound; ResolveDownload serves them
			// server-side. Records expire with the story URL they carry.
			if data, encodeErr := json.Marshal(storyRecord{Username: record.Profile.Username, URL: downloadURL, ContentType: contentType}); encodeErr == nil {
				p.cache.Set("tiktok:story:"+item.ID, data, 10*time.Minute)
			}
		}
		items = append(items, model.MediaItem{
			ID:           item.ID,
			Type:         model.Story,
			ThumbnailURL: cover,
			MediaURL:     mediaURL,
			Width:        int(item.Video.Width),
			Height:       int(item.Video.Height),
			Duration:     float64(item.Video.Duration),
			CreatedAt:    time.Unix(item.CreateTime, 0).UTC(),
			Caption:      item.Desc,
			Downloadable: true,
		})
	}
	return items, nil
}

// storyRecord carries the referer-bound story media URL from GetStories to
// ResolveDownload without any second upstream lookup.
type storyRecord struct {
	Username    string `json:"username"`
	URL         string `json:"url"`
	ContentType string `json:"contentType"`
}

// ResolveDownload serves one cached story media URL through the backend so the
// referer-bound CDN contract is met. IDs absent from the cache fail closed.
func (p *Provider) ResolveDownload(ctx context.Context, mediaID string) (*model.DownloadResource, error) {
	if !storyIDPattern.MatchString(mediaID) {
		return nil, httputil.Invalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.cache == nil {
		return nil, httputil.DownloadUnavailable
	}
	data, ok := p.cache.Get("tiktok:story:" + mediaID)
	if !ok {
		return nil, httputil.DownloadUnavailable
	}
	var record storyRecord
	if json.Unmarshal(data, &record) != nil || record.URL == "" || !validMediaURL(record.URL) || record.Username == "" || !usernamePattern.MatchString(record.Username) {
		return nil, httputil.DownloadUnavailable
	}
	return &model.DownloadResource{
		URL:          record.URL,
		Referer:      "https://www.tiktok.com/",
		AllowedHosts: []string{"*.tiktok.com", "*.tiktokcdn.com", "*.tiktokcdn-us.com", "*.tiktokcdn-eu.com"},
		ContentType:  record.ContentType,
		Username:     record.Username,
		Filename:     "ghostview-tiktok-" + mediaID,
	}, nil
}

func (*Provider) GetHighlights(context.Context, string) ([]model.Highlight, error) {
	return nil, httputil.Unsupported
}

