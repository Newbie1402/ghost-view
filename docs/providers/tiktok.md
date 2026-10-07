# TikTok provider verification

- Live status: **EXPERIMENTAL**. Public profile metadata AND active public stories are implemented and executed live. The post feed remains blocked by TikTok's request signing. Demo status: **MOCK** only when explicitly running fixtures.
- Last investigated/tested: **2026-10-07** (stories).
- Current mechanism: anonymous `GET https://www.tiktok.com/@<validated-username>`, parsing `__UNIVERSAL_DATA_FOR_REHYDRATION__` / `webapp.user-detail` from the returned public HTML. This is a public-page parser, not TikTok Display API and not an officially supported stable JSON contract.
- Request headers: `User-Agent: GhostView/1.0 (+public-content verification)` and `Accept: text/html,application/json`. No Cookie, Authorization, cookie jar, account login, or retained platform Set-Cookie values.
- Supported live operations: exact-username search and profile metadata. Access must have explicit `privateAccount` and a successful bootstrap status. Unknown privacy, challenge/login pages, restrictions, malformed data, unsafe redirects, and missing bootstrap fail closed.
- Stories mechanism (implemented, verified live 2026-10-07): `GET https://www.tiktok.com/api/story/item_list/?aid=1988&authorId=<numeric-id>&count=20&cursor=0&device_platform=web_pc` with only the identified GhostView UA and `Accept: application/json`. No signature (X-Bogus/X-Gnarly), no cookie, no login is required, unlike the post feed. `authorId` is the numeric user id already embedded in the profile bootstrap. Response JSON `itemList` items carry `video.playAddr`, `video.cover`, `duration`, `createTime`; non-matching authors, `privateItem`, and ads are skipped. Story playAddr URLs are Referer-bound (403 without `Referer: https://www.tiktok.com/`), so playback/downloads stream through the GhostView backend (`ResolveDownload` serves cached story URLs with that referer; records expire after 10 minutes). Covers are not referer-bound.
- Posts/reposts (opt-in browser mode, `GHOSTVIEW_TT_BROWSER=1`): the web feed APIs `/api/post/item_list/` and `/api/repost/item_list/` are signature-gated — replays with every signature stripped return HTTP 200 empty bodies (verified 2026-10-07) — and the known third-party API (tikwm) is Cloudflare-gated. The provider therefore optionally runs a local headless Chrome (`chromedp`, `--headless=new`) that loads the public profile and reads the feed payloads TikTok's own anonymous client fetches. TikTok challenges automated sessions with a captcha based on IP/session heat, so this mode is EXPERIMENTAL and unreliable by nature: it fails closed with UNAVAILABLE whenever the challenge appears (no anti-detection tuning is applied — defeating it would be circumvention).
- Unsupported live operations: display-name search, playable feed videos without the opt-in browser mode, highlights. Empty embedded `itemList` does not become a fake empty public feed.
- Posts blocker (re-verified 2026-10-06): the profile HTML embeds no `itemList` key under `webapp.user-detail` with desktop-browser, mobile-Safari, and application User-Agents; `GET /api/post/item_list/?aid=1988&secUid=...&count=10&cursor=0` returns HTTP 200 with an empty body without a valid client signature (X-Bogus/msToken), including variants with a dummy msToken and a `tt-target-idc=useast2a` cookie. GhostView does not generate or emulate signing fields; the documented oEmbed/creator-profile endpoints do not enumerate arbitrary accounts' posts.
- Stories blocker (2026-10-06): no public story endpoint is documented or reachable anonymously; the profile bootstrap exposes only a `UserStoryStatus` flag (observed 0 for the test account). Third-party wrappers use browser automation or signed clients, which GhostView does not reproduce.
- Known-video surface (observed, not productized): `GET https://www.tiktok.com/@<user>/video/<id>` anonymously embeds the full `itemStruct` (desc, duration, covers, author). Cover images on `p19-common-sign.tiktokcdn.com` fetched HTTP 200 image/jpeg; the `v16-webapp-prime.tiktok.com` playAddr/downloadAddr returned HTTP 403 Access Denied from this network even with page cookies. Because the public feed API is signed, no username→video-ID enumeration exists, so no download capability is claimed from this surface.
- Authentication: GhostView accepts no platform credentials. TikTok's [Display API](https://developers.tiktok.com/docs/en/display-api-get-started) requires owner OAuth, unlike its documented public [creator-profile oEmbed](https://developers.tiktok.com/docs/en/embed-creator-profiles).
- Candidate request: `GET https://www.tiktok.com/oembed?url=https%3A%2F%2Fwww.tiktok.com%2F%40scout2015`. No token, Cookie, session, or Authorization header is documented. Use an ordinary application User-Agent and `Accept: application/json`; neither creates a platform session.
- Candidate response: creator name/profile URL and embed markup. The documented JSON does not directly supply avatar, counts, a paginated video collection, raw playable media, stories, highlights, or downloads. GhostView must discard supplied HTML and must not infer these capabilities from TikTok's interactive embed.
- Access limits: private/underage accounts cannot be embedded according to the official document. Ambiguous denial cannot be called PRIVATE without explicit evidence; return UNAVAILABLE. The documented oEmbed quota is unspecified; respect 403/429 and timeouts without bypass.
- Rate limits: upstream public-page quota unknown. HTTP 429 produces RATE_LIMITED with an in-process cooldown honoring Retry-After; no automatic retry. Application default: 120 API requests/minute/client IP.
Live test input: `marcmarquez93`. `alex.morgan` remains only a MOCK fixture.

Actual root-run anonymous Go probe, 2026-10-06 (sanitized result; no credentials retained):

```text
GET https://www.tiktok.com/@marcmarquez93
HTTP 200; HTML bytes=417637
bootstrap statusCode=0; privateAccount=false
nickname=Marc Márquez; followerCount=3100000; followingCount=35; videoCount=349
webapp.user-detail keys: userInfo, shareMeta, statusCode, statusMsg, needFix (no itemList)
Creator oEmbed candidate: HTTP 429; text/plain; bytes=19; no retry
```

Actual opt-in live integration assertions executed 2026-10-07:

```text
GHOSTVIEW_LIVE_TEST=1 go test ./internal/provider/tiktok -run TestLivePublicTikTokStories -count=1 -v
--- PASS: TestLivePublicTikTokStories
    REAL TikTok stories username=thifchann14 items=1 firstID=7693420078263487765 kind=video/mp4 bytes=524288
GET /api/v1/profiles/tiktok/thifchann14/stories -> 1 real story; backend download streams HTTP 200 video/mp4 13,000,290 bytes
```

Earlier profile assertion executed 2026-10-06:

```text
GHOSTVIEW_LIVE_TEST=1 go test ./internal/provider/tiktok -run TestLivePublicTikTokProfile -count=1 -v
--- PASS: TestLivePublicTikTokProfile
    actual public profile: username=marcmarquez93 displayName="Marc Márquez" accessStatus=PUBLIC
    followers=3100000 following=35 posts=349 avatarHost=p16-common-sign.tiktokcdn.com
```

Actual local adapter test output, executed independently on 2026-10-06:

```text
go test -v ./internal/provider/... -count=1
--- PASS: TestPublicProfileCapabilities (0.00s)
--- PASS: TestParsePublicProfileAndMissingCounts (0.00s)
--- PASS: TestParsePrivateProfileStripsProtectedData (0.00s)
--- PASS: TestParseProfileFailsClosed (0.00s)
--- PASS: TestAvatarValidation (0.00s)
--- PASS: TestPublicRequestContractAndSearch (0.00s)
--- PASS: TestHTTPFailuresAndBounds (0.00s)
--- PASS: TestRateLimitHonorsCooldownWithoutRetry (0.00s)
--- PASS: TestContextCancellationAndTimeout (0.00s)
--- PASS: TestRedirectRestrictions (0.00s)
--- SKIP: TestLivePublicTikTokProfile (0.00s)
--- PASS: TestShortPublicProfileCacheAvoidsDuplicateLookup (0.00s)
--- PASS: TestPrivateProfilesAreNotCached (0.00s)
PASS
ok ghostview/internal/provider/tiktok 0.572s
```

The offline tests use controlled HTTP responses; their passing private case is not a claim of live private-account retrieval. The skipped opt-in test is not counted as live verification. Root's anonymous browser also received a public profile with empty items; platform SDK post requests yielded empty bodies and required dynamic signing/session query fields. GhostView does not reproduce signatures or sessions. [Full investigation](../REAL_PROVIDER_REPORT.md); [mock contract](mock.md).
