# TikTok provider verification

- Live status: **EXPERIMENTAL**. Public metadata probe succeeded; final live integration/UI verification is pending. Demo status: **MOCK** only when explicitly running fixtures.
- Last investigated/tested: **2026-10-06**.
- Current mechanism: anonymous `GET https://www.tiktok.com/@<validated-username>`, parsing `__UNIVERSAL_DATA_FOR_REHYDRATION__` / `webapp.user-detail` from the returned public HTML. This is a public-page parser, not TikTok Display API and not an officially supported stable JSON contract.
- Request headers: `User-Agent: GhostView/1.0 (+public-content verification)` and `Accept: text/html,application/json`. No Cookie, Authorization, cookie jar, account login, or retained platform Set-Cookie values.
- Supported live operations: exact-username search and profile metadata. Access must have explicit `privateAccount` and a successful bootstrap status. Unknown privacy, challenge/login pages, restrictions, malformed data, unsafe redirects, and missing bootstrap fail closed.
- Unsupported live operations: display-name search, posts, playable videos, stories, highlights, downloads. Empty embedded `itemList` does not become a fake empty public feed.
- Authentication: GhostView accepts no platform credentials. TikTok's [Display API](https://developers.tiktok.com/docs/en/display-api-get-started) requires owner OAuth, unlike its documented public [creator-profile oEmbed](https://developers.tiktok.com/docs/en/embed-creator-profiles).
- Candidate request: `GET https://www.tiktok.com/oembed?url=https%3A%2F%2Fwww.tiktok.com%2F%40scout2015`. No token, Cookie, session, or Authorization header is documented. Use an ordinary application User-Agent and `Accept: application/json`; neither creates a platform session.
- Candidate response: creator name/profile URL and embed markup. The documented JSON does not directly supply avatar, counts, a paginated video collection, raw playable media, stories, highlights, or downloads. GhostView must discard supplied HTML and must not infer these capabilities from TikTok's interactive embed.
- Access limits: private/underage accounts cannot be embedded according to the official document. Ambiguous denial cannot be called PRIVATE without explicit evidence; return UNAVAILABLE. The documented oEmbed quota is unspecified; respect 403/429 and timeouts without bypass.
- Rate limits: upstream public-page quota unknown. HTTP 429 produces RATE_LIMITED with an in-process cooldown honoring Retry-After; no automatic retry. Application default: 120 API requests/minute/client IP.
- Live test input: `marcmarquez93`. `alex.morgan` remains only a MOCK fixture.

Actual root-run anonymous Go probe, 2026-10-06 (sanitized result; no credentials retained):

```text
GET https://www.tiktok.com/@marcmarquez93
HTTP 200; HTML bytes=399097
bootstrap statusCode=0; privateAccount=false
nickname=Marc Márquez; followerCount=3100000; followingCount=35; videoCount=349
itemList=[]
Creator oEmbed candidate: HTTP 429; text/plain; bytes=19; no retry
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
