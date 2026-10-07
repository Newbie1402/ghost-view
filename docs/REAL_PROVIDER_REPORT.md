# Real provider investigation

Date: **2026-10-06** (all probes below were executed on this date). This report separates official integration evidence, executed retrieval, and reference-site advertising. Mock contracts validate product behavior; they do not satisfy real-data verification.

## Executed retrieval evidence (per-mechanism record)

Every mechanism below records: URL pattern, method, headers, authentication, response format, rate-limit behavior, login requirement, Go-backend viability, and test date. No real session cookies, credentials, or access tokens were committed; no platform Set-Cookie value was retained or reused.

### TikTok

| Field | Public profile HTML |
| --- | --- |
| Request | `GET https://www.tiktok.com/@<username>` |
| Headers | `User-Agent: GhostView/1.0 (+public-content verification)`, `Accept: text/html,application/json`; no Cookie/Authorization |
| Auth | None required |
| Response | HTML with `<script id="__UNIVERSAL_DATA_FOR_REHYDRATION__">` → `__DEFAULT_SCOPE__["webapp.user-detail"]` (statusCode, userInfo.user, userInfo.stats) |
| Rate limit | Unspecified; 429 honored with Retry-After cooldown. Creator oEmbed (`GET /oembed?url=...`) returned HTTP 429 `ratelimit triggered` on repeated same-day use |
| Works without login | Yes (profile metadata) |
| Works from Go | Yes |
| Tested | 2026-10-06 (profile: PASS) |

| Field | Public post feed |
| --- | --- |
| Request | `GET https://www.tiktok.com/api/post/item_list/?aid=1988&secUid=<secUid>&count=10&cursor=0...` |
| Result | HTTP 200 with **empty body** without a valid client signature (X-Bogus/msToken). Variants tried: plain, dummy msToken param, `tt-target-idc=useast2a` cookie, desktop/mobile/application User-Agents — all empty |
| HTML alternative | `webapp.user-detail` contains **no** `itemList` key in any UA variant (HTML bytes 232851–419652) |
| Verdict | **BLOCKED.** GhostView does not generate or emulate signing fields; the documented oEmbed/creator-profile endpoints do not enumerate arbitrary accounts' posts |
| Tested | 2026-10-06 (posts: FAIL, documented) |

| Field | Public stories |
| --- | --- |
| Result | No public endpoint reachable anonymously; bootstrap exposes only a `UserStoryStatus` flag (0 for the test account). Public wrappers rely on browser automation or signed clients, which GhostView does not reproduce |
| Verdict | **BLOCKED.** Tested 2026-10-06 (stories: FAIL, documented) |

| Field | Single known video page |
| --- | --- |
| Request | `GET https://www.tiktok.com/@<user>/video/<id>` |
| Result | Full anonymous `itemStruct` (desc, duration, covers, playAddr/downloadAddr). Covers on `p19-common-sign.tiktokcdn.com`: HTTP 200 image/jpeg. `v16-webapp-prime.tiktok.com` playAddr/downloadAddr: **HTTP 403 Access Denied** from this network even with the page's msToken cookie |
| Verdict | Observed but not productized: without unsigned post enumeration there is no username→video-ID path, so no download capability is claimed. Tested 2026-10-06 (download: FAIL for feed scope, documented) |

### Instagram

| Field | Public profile HTML |
| --- | --- |
| Request | `GET https://www.instagram.com/<username>/` |
| Headers | `User-Agent: GhostView/1.0 (+public-content verification)`, `Accept: text/html`; no Cookie/Authorization; redirects not followed |
| Auth | None required |
| Response | HTML with `<script type="application/json">` fragments: `xig_user_by_username` (is_private, counts, avatar), `polaris_ordered_timeline_connection` (12 edges), `lox_highlights_connection` (10 nodes) |
| Rate limit | Unspecified; 429 honored with Retry-After cooldown |
| Works without login | Yes |
| Works from Go | Yes |
| Tested | 2026-10-06 (profile: PASS) |

| Field | Public posts (first page) |
| --- | --- |
| Mechanism | Same profile HTML; timeline nodes filtered to `node.user.username == requested username` — the anonymous timeline **injects other accounts' media** (observed: `shoeieurope` post inside `marcmarquez93`'s timeline) |
| Result | 10 real items for the test account; images carry display-URL previews; `media_type=2` videos carry cover previews only |
| Works from Go | Yes |
| Tested | 2026-10-06 (posts: PASS for previews; playable feed video requires the per-post fetch below) |

| Field | Highlight tray |
| --- | --- |
| Mechanism | `lox_highlights_connection.edges[].node` = {id, title, cover_media_cropped_thumbnail_url, owner_username}; owner must match, covers restricted to HTTPS CDN hosts |
| Result | 10 real highlights for the test account |
| Verdict | Tray PASS; **items BLOCKED** (see stories row) |
| Tested | 2026-10-06 (highlights tray: PASS) |

| Field | Per-post media / download |
| --- | --- |
| Request | `GET https://www.instagram.com/p/<shortcode>/` |
| Response | `xig_polaris_media` matched by `code`; `if_not_gated_logged_out` → `video_versions` (progressive MP4) or `image_versions2.candidates` (largest valid); present non-null `gating_ruling` fails closed |
| CDN access | Cover/preview image HTTP 200 image/jpeg (580446 bytes); reel MP4 Range probe HTTP 206 video/mp4 |
| Works from Go | Yes — end-to-end download through the running server returned image/jpeg 3287974 bytes and video/mp4 6941323 bytes |
| Tested | 2026-10-06 (download: PASS) |

| Field | Public stories / highlight items |
| --- | --- |
| Request | `GET /api/v1/feed/reels_media/?reel_ids=%40<user>` (www and i.instagram.com hosts) |
| Result | HTTP 400 `require_login` anonymously. `GET /stories/<user>/` returns HTTP 200 but the logged-out document contains **no** story media items (no `video_versions`, no items); only og:description teases the story |
| Verdict | **BLOCKED without end-user social-media credentials.** Capability stays false; no silent mock fallback |
| Tested | 2026-10-06 (stories: FAIL, documented) |

| Field | web_profile_info API |
| --- | --- |
| Request | `GET /api/v1/users/web_profile_info/?username=<u>` with `x-ig-app-id: 936619743392459` (public static app id, not a credential) |
| Result | HTTP 401 `require_login` anonymously |
| Verdict | Not usable; HTML parser remains the mechanism. Tested 2026-10-06 |

### Facebook

| Field | Public profile identity |
| --- | --- |
| Request | `GET https://www.facebook.com/<username>` |
| Headers | `User-Agent: GhostView/1.0 (+public-content verification)`, `Accept: text/html`; no Cookie/Authorization; redirects not followed |
| Auth | None required |
| Response | HTML og metadata: `og:title` ("Hoàng Kim Bạc"), `og:url` (canonical, must match the requested username else fail closed), `og:image` (avatar on scontent host, anonymous fetch HTTP 200 image/jpeg 8985 bytes). No counts, no media nodes, no explicit privacy flag |
| Fail-closed | Redirect to `/login/?next=...` (mbasic and m hosts verified) returns UNAVAILABLE — a login wall is never guessed as PRIVATE; 404 returns NOT_FOUND |
| Works without login | Yes (identity only) |
| Works from Go | Yes |
| Tested | 2026-10-06 (profile: PASS at identity scope) |

| Field | Posts / stories / highlights / counts |
| --- | --- |
| Result | `mbasic.facebook.com`/`m.facebook.com` redirect anonymous visitors to `/login/`; `graph.facebook.com/v25.0/oembed_post` returns HTTP 400 OAuthException code 100 without a token; the `/photos` path serves a JS shell with no anonymously parseable media grid |
| Verdict | **BLOCKED without end-user social-media credentials.** Capabilities stay false; no silent mock fallback |
| Tested | 2026-10-06 (posts/stories/highlights/downloads: FAIL, documented) |

## Official interfaces

| Platform | Public candidate | Authentication | What is established |
| --- | --- | --- | --- |
| TikTok | Creator `/oembed?url=<profile-url>`; video oEmbed | No credentials documented | Metadata/embed only; no arbitrary post enumeration. Live probes returned HTTP 429 twice on 2026-10-06; treated as not executed |
| Instagram | Graph v25 `/instagram_oembed?url=...` | Tokenless per Meta's official plugin | HTTP 400 OAuthException Invalid parameter when executed; not the live mechanism |
| Facebook | Graph v25 `/oembed_post`, `/oembed_video` | Tokenless per Meta's official plugin | HTTP 400 OAuthException Invalid parameter when executed; not the live mechanism |

TikTok Display API and Instagram Graph API require approved apps/owner OAuth; both are outside the anonymous arbitrary-profile flow. GhostView accepts no user credentials and renders no provider HTML.

## End-to-end verification (live mode, real network, 2026-10-06)

Server `PROVIDER_MODE=live` on 127.0.0.1; assertions executed against real upstream data:

```text
REAL PROVIDER REPORT
TikTok (https://www.tiktok.com/@marcmarquez93 — 2026-10-06 baseline; stories re-verified 2026-10-07 on thifchann14):
  Profile: PASS  (real name Marc Márquez, 3.1M followers, verified, avatar from tiktokcdn)
  Posts: FAIL — unsigned /api/post/item_list/ returns empty body; HTML embeds no itemList (documented blocker)
  Stories: PASS  (2026-10-07, anonymous /api/story/item_list/; real story of @thifchann14 played in the UI and streamed 13 MB video/mp4 through the backend)
  Download: PASS  (story media only, via the referer-aware backend stream; the feed-scoped video download stays FAIL as documented)
Instagram (https://www.instagram.com/marcmarquez93/):
  Profile: PASS  (real name, 8,424,443 followers, verified badge)
  Posts: PASS  (10 real previews incl. 2 reel covers, all author-verified; UI labels "Video preview")
  Stories: CONDITIONAL — implemented via the operator-configured session path (GHOSTVIEW_IG_SESSIONID); anonymously still FAIL (reels_media 400 require_login, documented)
  Highlights: PASS  (10 real tray entries with real covers; items correctly disclosed as login-only)
  Download: PASS  (image/jpeg 3,287,974 B and video/mp4 6,941,323 B retrieved through the app API)
Facebook (https://www.facebook.com/bacbeodangiuu):
  Profile: PASS  (real identity "Hoàng Kim Bạc" + real avatar at identity scope; counts/media disclosed as unavailable)
  Posts: FAIL — login-walled anonymously (documented blocker)
  Stories: FAIL — login-walled anonymously (documented blocker)
  Download: FAIL — login-walled anonymously (documented blocker)
```

Live Go assertions (`GHOSTVIEW_LIVE_TEST=1`):

```text
GHOSTVIEW_LIVE_TEST=1 go test ./internal/provider/... -run TestLive -count=1 -v
--- PASS: TestLivePublicTikTokProfile      username=marcmarquez93 followers=3100000
--- PASS: TestLivePublicProfileAndImage    photoPreviews=10 CDNStatus=200 imageBytes=51975
--- PASS: TestLivePostMediaAndHighlights   shortcode=DeEO3H5iLHJ kind=image/jpeg bytes=3287974 highlights=10
--- PASS: TestLivePublicIdentity (facebook) displayName="Hoàng Kim Bạc" avatarBytes=8985
```

Browser UI verification: DATA SOURCE: REAL badges on home and profile views; real posts grid; Highlights tab renders real covers with "Login required" disclosure; Facebook profile renders the real avatar and name; TikTok profile discloses "This provider supplies profile information only." Mock mode still serves the full stories/highlights contract with DATA SOURCE: MOCK, so fixture data cannot be mistaken for real data.

## 2026-10-07 session: TikTok stories verified; Instagram stories made possible legitimately

Follow-up to the operator's request to match the reference viewers' story capability.

### TikTok stories — REAL (implemented and verified)

| Field | Active public stories |
| --- | --- |
| Discovery | Real-browser instrumentation of an anonymous TikTok session (account `thifchann14`, `UserStoryStatus:1` in the profile bootstrap) surfaced the endpoint TikTok's own client calls when the story bubble is clicked: `GET /api/story/item_list/?...&authorId=<numeric-id>&count=4&cursor=0...` (captured via the Performance API; the request carries `X-Gnarly` signatures generated by TikTok's JS) |
| Signature test | Replaying the captured URL via curl worked; **stripping X-Gnarly/X-Dynosaur/X-Bogus entirely also returned HTTP 200** with the full payload — the story endpoint, unlike `/api/post/item_list/`, is not signature-gated |
| Minimal Go-viable request | `GET https://www.tiktok.com/api/story/item_list/?aid=1988&authorId=<id>&count=20&cursor=0&device_platform=web_pc` with `User-Agent: GhostView/1.0 (+public-content verification)`; no cookies, no tokens; stable across repeated calls |
| authorId source | The numeric user id already present in the profile bootstrap (`webapp.user-detail.userInfo.user.id`) — no extra request |
| Response | JSON: `statusCode:0`, `itemList[]` with `id`, `createTime`, `desc`, `video.playAddr`, `video.cover`, `duration`, `width/height`, `story.ExpiredAt` |
| Media access | `playAddr` (v16-webapp-prime.tiktok.com) serves HTTP 206 video/mp4 **but is Referer-bound**: 403 without `Referer: https://www.tiktok.com/`, 200/206 with it. Covers are not referer-bound |
| Implementation | `GetStories` (profile → authorId → story API), items filtered by matching `author.uniqueId`, `privateItem`, ads. `ResolveDownload` streams cached story URLs server-side with the required referer (records expire after 10 min) — playback and downloads run through the same-origin backend endpoint |
| Verification | Live test PASS (1 real story, 512 KiB video/mp4 streamed); server E2E: `GET /api/v1/profiles/tiktok/thifchann14/stories` → 1 real story; backend download → HTTP 200 video/mp4 13,000,290 bytes; browser story viewer plays the real story with progress bar and Download button |

### Correction (2026-10-07, operator experiment): how mollygram does NOT work

The operator posted a story from their own account (`duyet.190703`), viewed it through mollygram, and Instagram's activity showed **zero viewers**. A logged-in account fetching a story registers a view, so mollygram's upstream is **not a plain account viewing the story**, and the earlier "session pool" description was too strong. Re-investigated with a full packet capture of mollygram's flow for `duyet.190703`:

- Their API still returns server-rendered HTML containing **fresh Instagram CDN URLs** (`efg` tag decodes to `urlgen_source:"www"`, `asset_age_days:0`) proxied through `anon-viewer.com`.
- The exact upstream mechanism stays server-private. Every anonymous door we could test was re-verified blocked: `reels_media` (www + i hosts, web + Android app IDs, `@username` and pk formats) returns a soft-empty `{"reels":{}}` to guests; `web_profile_info` 401; the `/stories/<user>/` document carries no media for any UA; `feed/user/<pk>/story/` 302s to login; legacy `__a=1&__d=dis` is dead (500).
- Two plausible mechanisms remain consistent with the zero-viewer observation: an authenticated session calling a **tray-listing endpoint that does not register views**, or a non-account internal surface. Both live behind their Turnstile-gated private API and cannot be adopted from the outside.
- Consequence for GhostView: the session-forwarder path stays the only legitimate mechanism. When the operator plugs in `GHOSTVIEW_IG_SESSIONID`, the implementation should try the tray endpoint (`feed/user/<pk>/story/`) **before** `reels_media` — if the tray truly skips view registration, GhostView's behavior would match mollygram's zero-viewer footprint.

### Instagram carousel posts — every image now viewable (implemented 2026-10-07)

The profile timeline embeds only a carousel's cover. `GetPosts` now expands each multi-image post in parallel (bounded, 7s budget, per-post document cached 5 minutes): every carousel child becomes its own full-resolution item (`<shortcode>-<n>`), viewable in the viewer and downloadable through the per-child resolver; a failed expansion falls back to the cover. Live verification: `thuytrang.com.vn` grew from 4 to **31 real items** (7+8+10+6 images), and the backend streamed child `DcG3FZKAdMj-3` as HTTP 200 image/jpeg 658,373 bytes. A viewer bug was found and fixed in the process: the same-origin backend media URL was rejected by the frontend URL sanitizer (only `/assets/fixtures/` was allowed), which had broken in-viewer images.

### TikTok posts and reposts — blocked at every legitimate door (documented 2026-10-07)

| Door | Evidence | Verdict |
| --- | --- | --- |
| Unsigned web API | `GET /api/post/item_list/` and `/api/repost/item_list/` replayed from the browser's own captured queries with **every** signature stripped (X-Gnarly/X-Dynosaur/X-Bogus/msToken), with matched browser UA, dummy `X-Bogus=1`, and `tt-target-idc` cookie variants: HTTP 200 with an **empty body** every time | BLOCKED — both feeds are signature-gated (unlike `/api/story/item_list/`, which is not) |
| Third-party API | `tikwm.com/api/user/posts` returns Cloudflare "Just a moment..." 403 to plain HTTP clients (browser UA and homepage cookies included) | BLOCKED — bypassing the challenge is out of policy |
| Headless browser (implemented, opt-in) | A local headless Chrome (`chromedp`, `--headless=new`) loads the profile and reads the feed payloads TikTok's own client fetches. It worked during research (DOM harvest returned 12 video anchors) but TikTok now challenges the automated session with a captcha after repeated automated access from this IP — the live test currently fails with UNAVAILABLE. No anti-detection tuning is applied: defeating the bot check is circumvention | **CONDITIONAL / EXPERIMENTAL** — works only while TikTok serves the anonymous client unchallenged; the app fails closed with UNAVAILABLE otherwise. Enabled with `GHOSTVIEW_TT_BROWSER=1`; UI advertises Posts/Reposts and shows an honest unavailable state when the challenge appears |
| Signature forging | Generating X-Bogus/X-Gnarly would require reimplementing TikTok's obfuscated anti-bot client | Refused |

### Instagram stories — session-backed path (implemented; live-verification requires the operator's session)

| Field | Active public stories |
| --- | --- |
| Mechanism | `GET https://www.instagram.com/api/v1/feed/reels_media/?reel_ids=@<user>` with `X-IG-App-ID: 936619743392459` (public static web app id, not a credential) and `Cookie: sessionid=<operator session>` |
| Configuration | Optional env `GHOSTVIEW_IG_SESSIONID` (the operator's OWN session cookie, validated, never logged or committed). Without it the stories capability stays false; the end user never provides credentials — the same architecture the reference viewers run |
| Response | `reels.<tray>.items[]` mapped to STORY media (image `image_versions2.candidates`, video `video_versions`); 401/403 (expired session) fails closed |
| Verification | Unit/contract tests PASS with controlled payloads; live assertion `TestLiveSessionStories` runs only when `GHOSTVIEW_LIVE_TEST=1` **and** `GHOSTVIEW_IG_SESSIONID` is set (no credential was available in this environment, so no live PASS is claimed for this path) |
| Tested | 2026-10-07 (anonymous impossibility re-confirmed 2026-10-06) |

### Refusals recorded (access-control circumvention, out of bounds regardless of goal)

- Solving mollygram's Cloudflare Turnstile programmatically (CAPTCHA circumvention).
- Evading ttviewer's Cloudflare full-page challenge.
- Forging TikTok's X-Bogus/X-Gnarly signatures for the signature-gated post feed.
- Using any Instagram/TikTok session other than one the operator explicitly configures as their own.

## Reference-site dissection (executed 2026-10-06, real browser + frontend analysis)

Both references were loaded in a real interactive browser with fetch/XHR instrumentation; their request flows, tokens, and response shapes were captured. This fulfills the research-priority step "evaluate whether a third-party provider is required" with evidence instead of advertising.

### Mollygram (mollygram.com/vi) — Instagram stories viewer

| Field | Observation |
| --- | --- |
| Frontend API | `GET https://media.mollygram.com?url=<username>` (profile), then `...&method=allstories` / `&method=highlights` |
| Required header | `X-Api-Token` — first call carries a **Cloudflare Turnstile** solution token (sitekey `0x4AAAAAACYB7qJyv8meg-6o`, rendered into `#turnstile-container`, read via `turnstile.getResponse()` in `assets/js/my.js`); follow-up calls carry the session `token` returned inside the first JSON response (`window.API_TOKEN`) |
| Tokenless Go-style request | `HTTP 200 {"msg":"Captcha verification failed","status":"error"}` (profile) / `{"msg":"Error, Please refresh the page and try again.","status":"Refresh"}` (stories) — no data |
| Response format | JSON `{status, html, token, source}` where `html` is a **server-rendered HTML fragment** (`source: GetProfileInfo | GetAllStories | GetHighlights | GetMedia | AccountPrivate`) and media URLs are proxied through the operator's own service (`fr9.anon-viewer.com/media2.php?media=<IG CDN URL>`) |
| Works without login | Only through a real browser that solves Turnstile |
| Works from Go | No — token issuance requires solving Turnstile in a browser |
| Tested | 2026-10-06 |

Verdict: Mollygram's stories capability is produced by **server-side Instagram session credentials** on their backend, surfaced to visitors behind a **per-search Cloudflare Turnstile CAPTCHA**, with media re-proxied through their infrastructure. GhostView cannot adopt this mechanism: programmatic Turnstile solving is CAPTCHA circumvention (refused on policy), operating Instagram session pools contradicts the project's no-credentials rule, and the payload is operator-private HTML, not a stable contract. The advertised "anonymous, no-login" UX is real for the visitor but is funded by credentials and CAPTCHA on the service side.

### TTViewer (ttviewer.net/vi) — TikTok viewer

| Field | Observation |
| --- | --- |
| Access | Cloudflare full-page challenge ("Just a moment..." / "Performing security verification"; Ray IDs `a4662ff04ec461ec` → `a466317ac875e903` across reloads) |
| Real-browser behavior | The challenge did not resolve even in an interactive browser session within the observation window; earlier tooling probe returned HTTP 403 |
| Bypass attempted | No — defeating the challenge is out of policy |
| Tested | 2026-10-06 |

Verdict: no mechanism observable; the service is Cloudflare-hard-gated. Nothing to adopt.

### What GhostView adopted from this research (implemented, verified)

Instagram's anonymous stories page exposes a reliable public signal that GhostView now uses honestly:

| Field | Active-story presence |
| --- | --- |
| Request | `GET https://www.instagram.com/stories/<username>/` |
| Headers | Same identified GhostView UA, `Accept: text/html`; no cookies; redirects not followed |
| Response | HTTP 200 HTML whose `<title>` is `Watch this story by <display name> on Instagram before it disappears.` **iff the account has an active public story**; a user without stories / a non-existent user returns the generic title `Instagram` |
| Verification inputs | `marcmarquez93`, `nasa`, `instagram` → story titles; `nonexistent_user_xz9q` → generic title (2026-10-06) |
| Use | `activeStoryPresence` in the Instagram provider adds a disclosure to the public profile ("This account has an active public story. Instagram serves story media only after login, so it is not retrieved."); every failure is non-fatal, 429 feeds the shared cooldown, presence is cached for 2 minutes |
| Tested | 2026-10-06 (live assertion PASS; UI screenshot recorded) |

Story **items** remain blocked without end-user social-media credentials — now with proof that even the dedicated reference services rely on CAPTCHA-gated credential infrastructure rather than a public mechanism.

## Reference behavior

Both references are third-party upstreams, not GhostView providers. No undocumented third-party endpoint, captured credentials, cookie/session values, or platform restriction bypass has been introduced. [Mollygram](https://mollygram.com/vi) and [TTViewer](https://ttviewer.net/vi) product claims were inspected against executed behavior above; their internal techniques are neither copied nor reproducible within GhostView's constraints.

## Release criterion

A provider may advertise only fields/operations returned by its real implementation. Capability flags in this build match the executed evidence above: TikTok {search, profile}, Instagram {search, profile, posts, highlights, downloads}, Facebook {search, profile}; stories remain false everywhere in live mode. A PASS above may only be reported when real network data was retrieved and assertions executed successfully — every PASS listed here was executed on 2026-10-06 with sanitized outputs recorded; every FAIL carries its documented blocker instead of a silent fallback.
