# Instagram provider verification

- Live status: **EXPERIMENTAL**. Real public profile, post previews, highlight tray, and per-post media retrieval are implemented and executed against the live site. Demo status: **MOCK** only when explicitly running fixtures.
- Last investigated/tested: **2026-10-06**.
- Mechanism: anonymous `GET https://www.instagram.com/<validated-username>/` and `GET https://www.instagram.com/p/<validated-shortcode>/`, reading JSON already embedded in the returned public HTML (`xig_user_by_username`, `lox_highlights_connection`, `xig_polaris_media`). These are public-page parsers, not a stable official Instagram API contract.
- Request headers: `User-Agent: GhostView/1.0 (+public-content verification)` and `Accept: text/html`. No Cookie, Authorization, cookie jar, login, private endpoint call, or retained platform Set-Cookie values. Redirects are not followed.
- Supported live operations:
  - Exact-username lookup and public profile metadata (`is_private` must be explicit; unknown visibility/restrictions fail closed; unavailable `all_media_count` is omitted, not rendered as zero).
  - First-page post previews. Image/carousel nodes carry the embedded display URL as preview; video nodes (`media_type=2`, `product_type=clips`) carry a cover thumbnail with no embedded playable URL. All feed items declare `previewOnly: true` and `downloadable: true`.
  - Highlight tray (`lox_highlights_connection`): id, title, and cover thumbnail only. Every node must prove `owner_username`; nodes without a validated public cover are dropped.
  - Per-post media resolution for download (`xig_polaris_media`): `video_versions` progressive MP4 or the largest `image_versions2` candidate. A present non-null `gating_ruling` or missing `if_not_gated_logged_out` block fails closed. The resolved media's `user.username` feeds the service-level public-profile check.
- Timeline ownership: the anonymous timeline can inject posts by other accounts (observed 2026-10-06: `DeIxNBLja9H` authored by `shoeieurope` inside `marcmarquez93`'s timeline). Every timeline node is now filtered by its `user.username`; unmatched nodes are ignored.
- Carousel expansion (implemented 2026-10-07): multi-image posts are expanded into one full-resolution item per child (`<shortcode>-<n>`) by fetching each post document in parallel (bounded, cached 5 min); a failed expansion falls back to the cover. Live-verified: 4 timeline entries expanded to 31 real images for the test account, per-child download HTTP 200 image/jpeg.
- Unsupported live operations: display-name search, later-page pagination, stories and highlight **items** (login-only; the operator-session path covers stories when configured), playback of bootstrap feeds without a post-page fetch.
- Stories blocker and conditional capability: `GET /api/v1/feed/reels_media/` returns HTTP 400 `require_login` anonymously (verified 2026-10-06); Instagram serves story items only to an authenticated session. Reference services (mollygram et al.) run server-side Instagram session pools behind Cloudflare Turnstile — [dissection evidence](../REAL_PROVIDER_REPORT.md). GhostView reproduces that architecture legitimately: when the operator sets `GHOSTVIEW_IG_SESSIONID` (their OWN session cookie, supplied server-side, never logged or committed), `GetStories` calls `GET /api/v1/feed/reels_media/?reel_ids=@<user>` with the public static `X-IG-App-ID` header and that cookie, maps `reels.<tray>.items` to STORY media (image candidates / video_versions), and fails closed on 401/403 (expired session). Without the variable the stories capability stays false; the end user never supplies credentials.
- Active-story disclosure (implemented 2026-10-06): `GET https://www.instagram.com/stories/<username>/` anonymously returns a title of the form "Watch this story by <display name> on Instagram before it disappears." iff the account has an active public story; otherwise the generic title "Instagram". The provider surfaces this as a non-fatal profile disclosure (`activeStoryPresence`, 2-minute cache, 429 feeds the shared cooldown); it is a signal only — items are never guessed.
- Authentication research: Meta's [official oEmbed plugin](https://github.com/facebook/meta-embeds-for-wordpress/blob/main/README.md) confirms tokenless access and includes Instagram profile/post/reel URLs. The candidate request `GET https://graph.facebook.com/v25.0/instagram_oembed?url=<validated-profile-url>` returned HTTP 400 JSON (OAuthException, Invalid parameter); it is not the live parser's mechanism. GhostView accepts no user credentials.
- Rate limits: upstream quota unknown. Application default: 120 API requests/minute/client IP; provider honors `Retry-After` on 429 with an in-process cooldown.
- Live test input: `marcmarquez93`. `alex.morgan` remains only a MOCK fixture.

Actual root-run anonymous probes, 2026-10-06 (sanitized results; no credentials retained):

```text
GET https://www.instagram.com/marcmarquez93/
HTTP 200; HTML bytes=861166
xig_user_by_username: is_private=false; follower_count=8424443; following_count=455
polaris_ordered_timeline_connection: 12 edges; 1 edge authored by shoeieurope (injected, filtered)
lox_highlights_connection: 10 highlight nodes (id, title, cover thumbnail)

GET https://www.instagram.com/p/DeEO3H5iLHJ/   (carousel)
HTTP 200; xig_polaris_media present; image_versions2 candidates=14; largest candidate HTTP 200 image/jpeg bytes=580446

GET https://www.instagram.com/reel/DdyWJmHtZ0z/
HTTP 200; video_versions=3 (progressive MP4, instagram.fsgn2-7.fna.fbcdn.net); Range probe HTTP 206 video/mp4

GET https://www.instagram.com/api/v1/feed/reels_media/?reel_ids=%40marcmarquez93
HTTP 400; {"message":"Please wait a few minutes before you try again.","require_login":true}

GET https://www.instagram.com/stories/marcmarquez93/
HTTP 200; document contains no story media items for anonymous visitors
```

Actual opt-in live integration assertions executed 2026-10-06:

```text
GHOSTVIEW_LIVE_TEST=1 go test ./internal/provider/instagram -run TestLive -count=1 -v
--- PASS: TestLivePublicProfileAndImage
    REAL Instagram username=marcmarquez93 access=PUBLIC photoPreviews=10 CDNStatus=200 imageBytes=51975
--- PASS: TestLivePostMediaAndHighlights
    REAL Instagram download shortcode=DeEO3H5iLHJ kind=image/jpeg bytes=3287974 highlights=10
```

End-to-end through the running GhostView server (live mode, 2026-10-06):

```text
GET /api/v1/media/instagram/DeEO3H5iLHJ/download -> HTTP 200; image/jpeg; 3287974 bytes (JPEG 3289x4096)
GET /api/v1/media/instagram/DdyWJmHtZ0z/download -> HTTP 200; video/mp4; 6941323 bytes (ISO Media MP4)
GET /api/v1/profiles/instagram/marcmarquez93/highlights -> 10 real highlight tray entries
GET /api/v1/profiles/instagram/marcmarquez93/stories -> UNSUPPORTED_CAPABILITY (honest, no mock fallback)
```

The browser UI displayed the REAL badge, real posts (including reel cover previews labeled "Video preview"), the highlight tray with per-highlight "Login required" state, and working per-post downloads. [Full investigation](../REAL_PROVIDER_REPORT.md); [mock contract](mock.md).
