package tiktok

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"ghostview/internal/httputil"
)

// BrowserFetcher runs a local headless Chrome that opens the public profile
// exactly like an anonymous visitor. TikTok's own client-side code generates
// and signs its feed requests; this fetcher only reads the responses the page
// already made on its own. No signatures are produced, no credentials are
// used, and nothing is submitted to the platform.
type BrowserFetcher struct {
	mu sync.Mutex // one browser run at a time
}

const chromeUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

type feedBodies struct {
	posts   []byte
	reposts []byte
}

const openRepostsTab = `(() => {
	const tabs = document.querySelectorAll('[data-e2e="user-tab-item"], [role="tab"]');
	for (const tab of tabs) { if (tab.textContent && tab.textContent.includes('Reposts')) { tab.click(); return true; } }
	return false;
})()`

// fetchFeed loads the profile and harvests the post and repost feed payloads
// that the page fetches itself. Either body may stay missing when the page
// never requested it; the caller decides how that fails closed.
func (b *BrowserFetcher) fetchFeed(ctx context.Context, username string) (*feedBodies, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Headless=new keeps the full modern Chrome engine: TikTok's own client
	// runs and issues its requests there, just like in a real visitor's
	// browser. The legacy --headless shell is gated by the platform.
	opts := make([]chromedp.ExecAllocatorOption, 0, len(chromedp.DefaultExecAllocatorOptions)+3)
	for _, opt := range chromedp.DefaultExecAllocatorOptions {
		opts = append(opts, opt)
	}
	opts = append(opts,
		chromedp.Flag("headless", "new"),
		chromedp.Flag("user-agent", chromeUA),
		chromedp.Flag("window-size", "1280,3000"),
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()
	runCtx, cancelRun := context.WithTimeout(browserCtx, 70*time.Second)
	defer cancelRun()

	// Subscribe before any navigation so no feed response is missed.
	evs := chromedp.Events(runCtx, network.ResponseReceived)
	found := map[string]network.RequestID{}
	var foundMu sync.Mutex
	collected := make(chan struct{})
	go func() {
		defer close(collected)
		for ev, err := range evs {
			if err != nil {
				return
			}
			kind := ""
			switch {
			case strings.Contains(ev.Response.URL, "/api/post/item_list/"):
				kind = "post"
			case strings.Contains(ev.Response.URL, "/api/repost/item_list/"):
				kind = "repost"
			default:
				continue
			}
			foundMu.Lock()
			if _, seen := found[kind]; !seen && ev.Response.Status == 200 {
				found[kind] = ev.RequestID
			}
			complete := len(found) == 2
			foundMu.Unlock()
			if complete {
				return
			}
		}
	}()

	if err := chromedp.Do(runCtx,
		chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
			_, err := cdp.Call(ctx, t, network.Enable, network.EnableParams{})
			return err
		}),
		chromedp.Navigate("https://www.tiktok.com/@"+url.PathEscape(username)),
		chromedp.WaitVisible(chromedp.CSSAll(`[data-e2e="user-post-item"]`)),
	); err != nil {
		return nil, httputil.Unavailable
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		// Re-issue the reposts tab click until its feed response arrives; the
		// post feed loads with the page itself.
		if _, err := chromedp.Run(runCtx, chromedp.Evaluate[bool](openRepostsTab)); err != nil {
			return nil, httputil.Unavailable
		}
		if err := chromedp.Do(runCtx, chromedp.Sleep(2500*time.Millisecond)); err != nil {
			return nil, httputil.Unavailable
		}
		foundMu.Lock()
		complete := len(found) == 2
		foundMu.Unlock()
		if complete {
			break
		}
	}
	select {
	case <-collected:
	case <-time.After(3 * time.Second):
	}

	bodies := &feedBodies{}
	for kind, id := range map[string]network.RequestID{"post": found["post"], "repost": found["repost"]} {
		if id == "" {
			continue
		}
		err := chromedp.Do(runCtx, chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
			res, err := cdp.Call(ctx, t, network.GetResponseBody, network.GetResponseBodyParams{RequestID: id})
			if err != nil {
				return err
			}
			if kind == "post" {
				bodies.posts = res.Body
			} else {
				bodies.reposts = res.Body
			}
			return nil
		}))
		if err != nil {
			continue
		}
	}
	if bodies.posts == nil && bodies.reposts == nil {
		return nil, httputil.Unavailable
	}
	return bodies, nil
}
