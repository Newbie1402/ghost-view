# Instagram provider verification

- Live status: **EXPERIMENTAL**. Public metadata/photo probe succeeded; final live integration/UI verification is pending. Demo status: **MOCK** only when explicitly running fixtures.
- Last investigated/tested: **2026-10-06**.
- Mechanism: anonymous `GET https://www.instagram.com/<validated-username>/`, reading JSON already embedded in the returned public HTML (`xig_user_by_username`). This is a public-page parser, not a stable official Instagram API contract.
- Request headers: `User-Agent: GhostView/1.0 (+public-content verification)` and `Accept: text/html`. No Cookie, Authorization, cookie jar, login, private endpoint call, or retained platform Set-Cookie values. Redirects are not followed.
- Supported live operations: exact-username lookup, public profile metadata, first-page image/carousel-cover previews where embedded. Preview items declare `previewOnly: true`. `is_private` must be explicit; unknown visibility/restrictions fail closed. Unavailable `all_media_count` is omitted, not rendered as zero.
- Unsupported live operations: display-name search, later-page pagination, complete carousels, playable videos/reels, stories, highlights, downloads. A carousel cover is one preview, not its full collection. No fabricated cursor is returned.
- Authentication research: Meta's [official oEmbed plugin](https://github.com/facebook/meta-embeds-for-wordpress/blob/main/README.md) confirms tokenless access and includes Instagram profile/post/reel URLs. This updates the earlier blanket uncertainty about authentication. GhostView accepts no user credentials.
- Candidate request: `GET https://graph.facebook.com/v25.0/instagram_oembed?url=<validated-profile-url>`. An anonymous Go test returned HTTP 400 JSON; this candidate is not the live parser's integration mechanism.
- The interface returns embed markup, not a documented story/highlight collection or download license. Raw HTML/scripts cannot be rendered directly under GhostView's security/privacy contract.
- Research: direct [Instagram oEmbed docs](https://developers.facebook.com/docs/instagram-platform/oembed/) returned HTTP 429 on 2026-10-06. No retry/bypass followed. The official repository was inspected instead; no third-party mirror was relied upon.
- Rate limits: upstream quota unknown; documentation could not be inspected. Application default: 120 API requests/minute/client IP.
- Live test input: `marcmarquez93`. `alex.morgan` remains only a MOCK fixture.

Actual root-run anonymous Go probe, 2026-10-06 (sanitized result; no credentials retained):

```text
GET https://www.instagram.com/marcmarquez93/
HTTP 200; HTML bytes=862382
xig_user_by_username: is_private=false
follower_count=8424528; following_count=455
polaris_ordered_timeline_connection: 12 edges (10 carousel, 2 video)
First public display image: HTTP 200; image/jpeg; bytes=51360; JPEG magic asserted
Official Graph v25 profile oEmbed candidate: HTTP 400; JSON code=100; OAuthException; Invalid parameter
```

Actual local parser test output, executed 2026-10-06:

```text
go test -v ./internal/provider/... -count=1
--- PASS: TestPublicBootstrap (0.00s)
--- PASS: TestPrivateBootstrap (0.00s)
--- PASS: TestFailClosed (0.00s)
--- PASS: TestImageURLValidation (0.00s)
--- PASS: TestHTTPContractAndBundleCache (0.00s)
--- PASS: TestHTTPFailures (0.00s)
--- SKIP: TestLivePublicProfileAndImage (0.00s)
PASS
ok ghostview/internal/provider/instagram 0.302s
```

This is the Instagram parser package excerpt from the final passing local provider-suite run. The unsupported-download contract mismatch was corrected and rerun successfully. The opt-in network test above was skipped, not verified.

Actual opt-in integration assertion executed by the Instagram implementation agent on 2026-10-06:

```text
GHOSTVIEW_LIVE_TEST=1 go test ./internal/provider/instagram -run TestLivePublicProfileAndImage -count=1 -v
username=marcmarquez93 access=PUBLIC photoPreviews=9 CDNStatus=200 imageBytes=51360
--- PASS: TestLivePublicProfileAndImage (1.17s)
```

Live page content varies; the initial public bootstrap probe had ten image/carousel entries, while the subsequent integration assertion returned nine previews. The actual integration test proves public profile/photo retrieval without login and successful JPEG retrieval. It does not establish API/UI end-to-end success, private-account retrieval, video playback, stories, or download permission. Earlier UNAVAILABLE adapter contracts are superseded by the new parser. [Full investigation](../REAL_PROVIDER_REPORT.md); [mock contract](mock.md).
