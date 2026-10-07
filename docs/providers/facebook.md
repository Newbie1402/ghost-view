# Facebook provider verification

- Live status: **EXPERIMENTAL**. Real public identity (display name, avatar, canonical URL) is implemented and executed against the live site. Demo status: **MOCK** only when explicitly running fixtures.
- Last investigated/tested: **2026-10-06**.
- Mechanism: anonymous `GET https://www.facebook.com/<validated-username>`, trusting only Open Graph metadata (`og:title`, `og:image`, `og:url`, `og:image:alt`) whose canonical URL matches the requested username. This is a public-page parser, not an official API contract.
- Request headers: `User-Agent: GhostView/1.0 (+public-content verification)` and `Accept: text/html`. No Cookie, Authorization, cookie jar, login, or retained platform Set-Cookie values. Redirects are not followed.
- Supported live operations: exact-username search and public profile identity. Display name is taken from `og:title` (bullet/vanity suffix stripped); avatar from `og:image` restricted to `*.fbcdn.net` / `*.cdninstagram.com` HTTPS hosts; no follower/media counts are invented (Facebook serves them only inside login-walled surfaces, so the fields stay nil).
- Fail-closed behavior: a redirect to `/login/` cannot distinguish a private profile from a login wall and returns UNAVAILABLE (never a guessed PRIVATE); 404 returns NOT_FOUND; "content not found" shells without matching og metadata fail closed.
- Unsupported live operations: posts, stories, highlights, downloads, counts, and any non-exact search. `mbasic.facebook.com` and `m.facebook.com` redirect anonymous visitors to `/login/` (verified 2026-10-06), so no media surface is reachable without credentials.
- Authentication research: Meta's [official oEmbed plugin](https://github.com/facebook/meta-embeds-for-wordpress/blob/main/README.md) confirms tokenless post/reel embeds. The candidate `GET https://graph.facebook.com/v25.0/oembed_post?url=...` returned HTTP 400 JSON (code 100, OAuthException, Invalid parameter) when tested again on 2026-10-06; it is not the live parser's mechanism. GhostView accepts no user credentials.
- Rate limits: upstream quota unknown. Application default: 120 API requests/minute/client IP; provider honors `Retry-After` on 429 with an in-process cooldown and caches successful identities for one minute.
- Live test input: `https://www.facebook.com/bacbeodangiuu`. `alex.morgan` exercises MOCK fixtures only.

Actual root-run anonymous probes, 2026-10-06 (sanitized results; no credentials retained):

```text
GET https://www.facebook.com/bacbeodangiuu
HTTP 200; text/html; bytes=624241
og:title="Hoàng Kim Bạc"; og:url=https://www.facebook.com/bacbeodangiuu/
og:image=scontent.fsgn2-5.fna.fbcdn.net/v/t39.30808-1/... ; anonymous fetch HTTP 200 image/jpeg bytes=8985
og:description="Hoàng Kim Bạc đang ở trên Facebook..." (no counts)
entity userID 61593170699752 embedded; no follower/fan counts, no media nodes

GET https://mbasic.facebook.com/bacbeodangiuu -> HTTP 302 -> /login/?next=... (fail closed)
GET https://m.facebook.com/bacbeodangiuu      -> HTTP 302 -> /login/?next=... (fail closed)
GET https://graph.facebook.com/v25.0/oembed_post?url=... -> HTTP 400 OAuthException code=100
```

Actual opt-in live integration assertion executed 2026-10-06:

```text
GHOSTVIEW_LIVE_TEST=1 go test ./internal/provider/facebook -run TestLivePublicIdentity -count=1 -v
--- PASS: TestLivePublicIdentity
    REAL Facebook username=bacbeodangiuu displayName="Hoàng Kim Bạc" avatarBytes=8985
```

End-to-end through the running GhostView server (live mode, 2026-10-06): `GET /api/v1/profiles/facebook/bacbeodangiuu` returned the real identity with `dataSource: REAL`, `accessStatus: PUBLIC`, and the disclosed limitation message; the browser UI rendered the real avatar and name. The earlier UNAVAILABLE decision was based on "identity alone is insufficient"; the implemented scope now claims exactly that identity (no counts, no media), which the anonymous og metadata does establish. [Full investigation](../REAL_PROVIDER_REPORT.md); [mock contract](mock.md).
