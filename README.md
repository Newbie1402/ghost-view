# GhostView

A responsive public social-content viewer using Go 1.27.1, `net/http`, semantic HTML, CSS and vanilla JavaScript. No third-party Go dependencies.

**The application defaults to REAL providers. It never silently falls back to fixtures.** TikTok public profiles, Instagram public profiles/post previews/highlight trays/per-post media downloads and Facebook public identities were retrieved through anonymous Go requests and exercised through the frontend. Every screen identifies `DATA SOURCE: REAL` or `MOCK`.

Executed results, exact capability scope and blockers: [Real provider report](docs/REAL_PROVIDER_REPORT.md). The [initial implementation report](docs/IMPLEMENTATION_REPORT.md) records the earlier mock-only milestone and is superseded for live capabilities.

## Setup

Install Go 1.27.1 or newer:

```sh
cp .env.example .env
set -a
. ./.env
set +a
make run
```

Open [GhostView](http://localhost:8080). Environment files are not loaded automatically by Go. To avoid an occupied port:

```sh
ADDR=127.0.0.1:18080 make run
```

Real inputs exercised:

- `https://www.tiktok.com/@marcmarquez93?_r=1&_t=ZS-9AKf9BKqZhS`
- `https://www.instagram.com/marcmarquez93/`
- `https://www.facebook.com/bacbeodangiuu` — returns UNAVAILABLE, not mock data.

Live search resolves exact usernames/profile URLs. Display-name discovery is unsupported. A recognized URL determines the platform regardless of the selected browser control. TikTok `_r` and `_t` share-tracking parameters are discarded and never forwarded upstream; arbitrary query parameters remain rejected.

Explicit fixture mode:

```sh
PROVIDER_MODE=mock make run
```

Only fixture mode supplies `alex.morgan`, ambiguous `alex`, `private.user`, mock stories/highlights and mock image downloads. It displays `DATA SOURCE: MOCK`.

## Architecture

```text
cmd/server/main.go                 configuration, wiring, graceful shutdown
internal/handler                   thin HTTP routes, envelopes, static assets
internal/service                   normalization, access/capability checks, provenance
internal/provider                  replaceable SocialProvider contract
  tiktok                           public profile HTML/bootstrap JSON
  instagram                        public profile JSON and image-preview timeline
  facebook                         explicit unavailable adapter
  mock                             deterministic development fixtures
internal/model                     unified profile/media/capability models
internal/downloader                validated, bounded attachment retrieval
internal/cache                     replaceable bounded in-memory TTL cache
internal/middleware                headers, CORS, body/method/rate limits, safe logs
internal/config                    validated environment configuration
internal/httputil                  safe errors, JSON envelopes, Retry-After handling
web                                framework-free responsive frontend
scripts                            mock and real curl acceptance checks
tests                              source and browser smoke checks
```

Handlers → service → providers. Request contexts and deadlines propagate; provider response bodies are bounded; server timeouts and bounded graceful shutdown protect resources. Provider models never leak into responses.

The service stamps `dataSource` from the provider status, overriding provider-supplied claims. Cache values are serialized copies. Service TTLs: profiles5minutes, posts2minutes, stories45seconds, search/highlights2minutes. TikTok additionally reuses normalized public profiles for30seconds; Instagram reuses normalized profile/preview bundles for60seconds to avoid repeated HTML requests. Raw HTML/cookies are not stored. Replace `cache.Cache` for Redis/shared storage.

## Actual capabilities

`PROVIDER_MODE=live`:

| Platform | Profile | Posts | Stories | Highlights | Download | Status |
| --- | --- | --- | --- | --- | --- | --- |
| TikTok | Real | EXPERIMENTAL opt-in headless-browser mode (`GHOSTVIEW_TT_BROWSER=1`); unreliable — TikTok challenges it with a captcha | **Real** (active public stories; playback streams through the backend) | Unavailable | Real story media | See current verification report/API |
| Instagram | Real | First-page previews incl. reel covers | Real when the operator sets `GHOSTVIEW_IG_SESSIONID`; otherwise blocked, login-only | Real tray (titles + covers; items via session only) | Real per-post media (image and MP4) | See current verification report/API |
| Facebook | Real identity (name + avatar) | Unavailable | Unavailable | Unavailable | Unavailable | See current verification report/API |

The provider report and `/api/v1/providers` give the exact current verification designation. Instagram feed items are author-verified (the anonymous timeline can inject other accounts' media) and labeled `Image preview`/`Video preview` with `previewOnly: true`; per-post download resolves the media manifest from the public post page. Instagram story items and highlight items require a platform session and are never fetched. Facebook identity comes from the public document's Open Graph metadata; counts and media stay nil instead of being invented. Unknown timestamps/counts/dimensions are omitted; login walls fail closed instead of being called PRIVATE. No fabricated cursors are returned.

`PROVIDER_MODE=mock`:

| Platform | Profile | Posts | Stories | Highlights | Download | Status |
| --- | --- | --- | --- | --- | --- | --- |
| TikTok | Fixture | Fixture images/reels | Fixture image/video | Fixture | Fixture images | MOCK |
| Instagram | Fixture | Fixture images/reels | Fixture image/video | Fixture | Fixture images | MOCK |
| Facebook | Fixture | Fixture images/reels | Fixture image/video | Fixture | Fixture images | MOCK |

The UI consumes declared capabilities and does not display unsupported tabs or downloads. A provider is VERIFIED only for its executed capabilities; that status is not a promise that every platform feature or every username is retrievable. Platform restrictions/schema changes produce PRIVATE or UNAVAILABLE, never fallback data.

## Provider mechanism and limitations

TikTok exposes profile data in `__UNIVERSAL_DATA_FOR_REHYDRATION__` on the ordinary public profile page. Instagram exposes explicit `is_private` profile data, post previews, the highlight tray and per-post media manifests in `application/json` bootstrap scripts and public post pages. Facebook exposes public identity through its anonymous profile document's Open Graph metadata. All integrations use an identified `GhostView/1.0` client without cookies, authorization, login, signature generation or browser emulation. Missing privacy flags, wrong identity, malformed data, private/unpublished content, gated media and access walls fail closed.

Official integrations were investigated first. TikTok's [Display API](https://developers.tiktok.com/docs/en/display-api-overview) is owner-authorized, while its documented [creator oEmbed](https://developers.tiktok.com/docs/en/embed-creator-profiles) probe returned 429. [Meta's official oEmbed plugin](https://github.com/facebook/meta-embeds-for-wordpress) documents tokenless endpoints; the supplied Instagram profile returned 400 InvalidParameter. The working integrations read the public documents rather than emulate restricted internal APIs. Precise patterns, headers, response shapes, rate behavior, actual outputs and dates are in [provider verification documents](docs/providers/).

[Mollygram](https://mollygram.com/vi) and [TTViewer](https://ttviewer.net/vi) are behavioral references only. Their service endpoints are not used as data sources. Referenced product claims are distinguished from verified GhostView behavior in the real provider report.

To extend a provider, implement `SocialProvider`, declare only delivered capabilities, enforce access/identity validation, honor context and rate restrictions, and return unified models and safe typed errors. Add unit/contract tests plus opt-in genuine network assertions. Never mark synthetic/parser tests as real retrieval success.

## Environment

| Variable | Default | Purpose |
| --- | --- | --- |
| `ADDR` | `:8080` | Listener; use `127.0.0.1:18080` for local-only access |
| `PROVIDER_MODE` | `live` | REAL integrations or explicit `mock` fixtures |
| `WEB_DIR` | `web` | Static frontend directory |
| `PROVIDER_TIMEOUT` | `8s` | Deadline per provider operation |
| `DOWNLOAD_TIMEOUT` | `20s` | Attachment deadline; example/Compose uses15s |
| `DOWNLOAD_MAX_BYTES` | `26214400` | Attachment cap; configuration maximum100MiB |
| `RATE_LIMIT_PER_MINUTE` | `120` | API requests/client IP/fixed minute window |
| `CACHE_MAX_ENTRIES` | `1000` | Process-local service cache entries |
| `SHUTDOWN_TIMEOUT` | `10s` | Shutdown deadline |
| `CORS_ALLOWED_ORIGINS` | empty | Exact comma-separated allowed browser origins |

Invalid configuration stops startup. Durations are constrained to1ms–5minutes. The app serves HTTP; production hosting must supply TLS termination. Upstream429 responses cause bounded provider cooldown honoring `Retry-After`; there is no automatic upstream retry.

## API

| Method/path | Result |
| --- | --- |
| `GET /api/v1/health` | Status/configured mode |
| `GET /api/v1/providers` | Platform status, data source and capabilities |
| `GET /api/v1/search?platform=tiktok&q=...` | Normalized platform/provenance/candidates |
| `GET /api/v1/profiles/{platform}/{username}` | Profile, provenance and access status |
| `GET /api/v1/profiles/{platform}/{username}/posts?cursor=...` | Supported media page |
| `GET /api/v1/profiles/{platform}/{username}/stories` | Supported stories only |
| `GET /api/v1/profiles/{platform}/{username}/highlights` | Supported highlights only |
| `GET /api/v1/media/{platform}/{mediaId}/download` | Supported validated attachment only |

JSON success: `{"data":{},"error":null}`. Failure: `{"data":null,"error":{"code":"PROFILE_NOT_FOUND","message":"Profile was not found."}}`.

Invalid input400; missing profile404; protected media403; unsupported capability422; unavailable503; rate limit429; provider timeout504. Private profile metadata is HTTP200 with `accessStatus:"PRIVATE"`, stripped protected fields and explicit explanation. No internal provider messages/credentials are exposed. Unknown information is omitted, not shown as zero.

```sh
curl -sG 'http://localhost:8080/api/v1/search' --data-urlencode platform=instagram --data-urlencode q=https://www.instagram.com/marcmarquez93/
curl -s 'http://localhost:8080/api/v1/profiles/instagram/marcmarquez93/posts'
```

## Testing

```sh
make fmt
make vet
make test                  # race checks; network tests skip unless opted in
make build
make acceptance            # isolated explicit MOCK server, actual curl assertions
make integration
make smoke                 # source/assets/syntax/payload checks
make live-acceptance       # isolated REAL server; Instagram profile + actual CDN JPEG
LIVE_PLATFORM=tiktok make live-acceptance
GHOSTVIEW_LIVE_TEST=1 go test -v ./internal/provider/tiktok ./internal/provider/instagram -run TestLive -count=1
```

Acceptance requires Go, curl, Python3 and a free local port; scripts select one and clean up only their own process. Real acceptance never converts upstream failure into mock success. Its media test validates HTTPS/domain/publicDNS, pins the validated address, asserts HTTP200, MIME, JPEG magic and actual byte length, and prints a hash instead of a signed URL.

Optional browser checks require Playwright and Chrome/Chromium installed separately in development:

```sh
# Start REAL application first:
BASE_URL=http://127.0.0.1:8080 make browser-real
# Start an explicitly MOCK application for the fixture browser suite:
BASE_URL=http://127.0.0.1:8080 make browser
# Optional environment: PLAYWRIGHT_MODULE=/absolute/path/to/playwright
# Optional environment: CHROME_PATH=/absolute/path/to/chrome
```

Browser suites use1440×900 and390×844, save screenshots under `artifacts/screenshots/`, and assert actual DOM/media decoding, provenance, supported controls, errors and overflow. Dedicated fixture regressions test safe text rendering, video-ended progression and focus restoration; those do not certify a live feature. Manrope is self-hosted under [SIL OFL](web/assets/fonts/OFL.txt); no Google font request occurs at runtime.

## Docker

```sh
docker compose up --build
docker compose down
```

Go multi-stage build; non-root runtime; Compose read-only filesystem, dropped capabilities, `no-new-privileges` and health check. Docker defaults to LIVE. Set `PROVIDER_MODE=mock` explicitly for fixtures. `CORS_ALLOWED_ORIGINS`/mode can be overridden from `.env`; other Compose settings are declared in the file. Port8080 may need remapping if occupied.

## Privacy and security

No platform passwords, cookies, access tokens or sessions are requested, retained or replayed. No private API credential harvesting, fake sessions, CAPTCHA solving, authentication bypass, proxy rotation or rate-limit bypass exists. Unsupported restricted endpoints remain unsupported. The media viewer uses public image requests without provider HTML or tracking SDKs; no deliberate platform view-registration mechanism is implemented.

This is not anonymity from the operator or CDN: server/hosting infrastructure can observe requests, and direct media requests expose the viewer's network information to the public media host. `no-referrer` avoids disclosing the GhostView URL. Future integrations require the same privacy/security review.

Provider text uses `textContent`/DOM nodes. CSP allows only local scripts/styles/connections, local video fixtures, and explicitly listed HTTPS social CDN images. It never loads TikTok/Meta embed SDKs or injects provider HTML. Headers include `nosniff`, `no-referrer`, restrictive Permissions-Policy, frame protection and same-origin resource policy. CORS is exact-origin, credential-free. GET body sizes, including chunked bodies, are bounded4KiB; methods/queries/identifiers/server timeouts are validated. Logs omit usernames, query strings, cookie/token values and provider bodies.

Downloads resolve provider media IDs, validate the owner/access status and allowlisted HTTPS resource, reject every nonpublic DNS result, pin the IP for dialing, validate each redirect, bound time/bytes, sniff MIME and generate safe attachment filenames. Remote SVG/HTML is rejected; trusted local mock SVG is permitted. Real downloads remain disabled because permission/retrievability has not been verified.

Cache/limits are process-local; arbitrary forwarding headers are ignored. Multiple replicas/public deployments need trusted edge limits/shared caching. Profile privacy may remain cached for up to5minutes; no instantaneous change-detection promise is made. Public signed media URLs may expire. Bootstrap schemas can change, and failure returns unavailable. Public visibility alone does not establish redistribution/download rights. Concurrent bounded downloads consume memory; no third-party provider or paid extraction job is configured.
