# GhostView

A responsive, all-light interface for exploring public social content through independently replaceable providers. Go 1.27.1, `net/http`, HTML, CSS, and vanilla JavaScript; no third-party Go dependencies.

**The default application is a working, explicitly labeled demo. TikTok, Instagram, and Facebook live retrieval is UNAVAILABLE.** Deterministic fixtures validate search, profile selection, private states, pagination, image/video stories, highlights, and fixture downloads. They are not actual social accounts or live platform data.

Executed verification and limitations: [Implementation report](docs/IMPLEMENTATION_REPORT.md).

## Setup

Install Go 1.27.1 or newer, then from the repository root:

```sh
cp .env.example .env
set -a
. ./.env
set +a
make run
```

Open [GhostView](http://localhost:8080). Go reads process environment variables; it does not automatically load `.env`. Alternatively, run `make run` directly with the documented defaults.

Try `@alex.morgan` for a public demo, `alex` or `Alex Morgan` for two candidates, `private.user` for a private account, and `nobody` for no results. All three platform selectors use the same local fixtures. `unavailable`, `rate.limited`, and `timeout` exercise error states. Mock image downloads are generated SVG fixtures; mock video downloads are unavailable.

## Architecture

```text
cmd/server/main.go       configuration, dependency wiring, graceful shutdown
internal/model          unified profiles, media, capabilities, access statuses
internal/handler        HTTP routing, envelopes, static files, attachments
internal/service        normalization, access checks, capability checks, caching
internal/provider       SocialProvider contract and unavailable adapters
  mock                  deterministic fixture provider
  tiktok/instagram/facebook   replaceable live adapter entry points
internal/downloader     allowlisted HTTPS retrieval, DNS/redirect validation
internal/cache          bounded, replaceable in-memory TTL cache
internal/middleware     headers, CORS, limits, rate limits, structured logs
internal/config         validated environment configuration
internal/httputil       safe JSON responses and typed public errors
web                     semantic HTML, CSS, JavaScript, local media
scripts                 curl acceptance and integration checks
tests                   frontend smoke checks
```

Handlers call the service; the service calls providers through `SocialProvider`. HTTP responses use domain models, not platform API models. Request contexts reach providers and downloads. Provider calls have deadlines; the HTTP server has header/read/write/idle timeouts and handles SIGINT/SIGTERM with bounded graceful shutdown.

The cache stores serialized copies under platform/resource/username/cursor keys: profiles 5 minutes, posts 2 minutes, stories 45 seconds, highlights/search 2 minutes. Entries are bounded and expire. Replace the `cache.Cache` implementation for shared storage; restart clears the current process cache.

## Environment

| Variable | Default | Purpose |
| --- | --- | --- |
| `ADDR` | `:8080` | Listening host and port; use `127.0.0.1:8080` for local-only access |
| `PROVIDER_MODE` | `mock` | `mock` fixtures or `live` unavailable adapters |
| `WEB_DIR` | `web` | Static frontend directory |
| `PROVIDER_TIMEOUT` | `8s` | Deadline for each provider operation |
| `DOWNLOAD_TIMEOUT` | `20s` | Download deadline; example/Compose uses `15s` |
| `DOWNLOAD_MAX_BYTES` | `26214400` | Maximum attachment size; configured limit cannot exceed 100 MiB |
| `RATE_LIMIT_PER_MINUTE` | `120` | Per-client-IP API requests in a fixed window |
| `CACHE_MAX_ENTRIES` | `1000` | Maximum process-local cache entries |
| `SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown deadline |
| `CORS_ALLOWED_ORIGINS` | empty | Comma-separated exact browser origins; no wildcard or credentials |

Durations must be between 1 ms and 5 minutes; invalid configuration stops startup. Empty CORS configuration exposes no cross-origin permission. Deployment behind TLS termination is recommended; the app serves plain HTTP and does not manage certificates.

## Development and testing

```sh
make fmt
make vet
make test             # Go tests, race detector, coverage
make build            # bin/ghostview
make acceptance       # builds, starts isolated server, curl assertions, cleanup
make integration      # provider/HTTP/download tests and acceptance script
make smoke            # Python 3 frontend smoke checks
make check            # vet, tests, build, acceptance, smoke
```

Acceptance requires `curl`, Python 3, Go, and an available port (automatically selected unused local port; override with `ACCEPTANCE_PORT`). It executes assertions for health, exact/ambiguous search, public/private handling, posts, stories/highlights, fixture download, SSRF rejection, and headers; failures exit nonzero. Mock provider contracts test all three platform adapters. Live adapter contract tests verify **UNAVAILABLE**, not successful real retrieval. Provider-specific evidence is in [docs/providers](docs/providers).

The optional browser suite requires Playwright and Chromium/Chrome already available in your development environment:

```sh
BASE_URL=http://127.0.0.1:8080 make browser
# Optional: PLAYWRIGHT_MODULE=/absolute/path/to/playwright
# Optional: CHROME_PATH=/absolute/path/to/chrome
```

Run the application first. The browser suite checks real demo APIs, downloads and playback at both sizes; dedicated altered-response regressions exercise safe text rendering and video-before-image progression. It writes screenshots to `artifacts/screenshots/`. The self-hosted Manrope heading font is licensed under [SIL OFL](web/assets/fonts/OFL.txt); no remote font requests occur.

Browser validation covers 1440×900 and 390×844, search, candidates, galleries, story controls, downloads, private/error/loading states, keyboard behavior, and horizontal overflow. Screenshot artifacts belong in `artifacts/screenshots/`. Build success alone does not establish browser or Docker validation.

## Docker

```sh
docker compose up --build
docker compose down
```

The multi-stage Dockerfile builds a static Go binary and runs as a non-root user. Compose drops capabilities, enables a read-only filesystem and `no-new-privileges`, and checks health. The Compose environment supports `PROVIDER_MODE` and `CORS_ALLOWED_ORIGINS` overrides from `.env`; other values are explicitly set in the Compose file. Docker requires a locally available daemon and access to the base-image registry. See the implementation report for whether containers were actually executed.

## API

| Method/path | Input/result |
| --- | --- |
| `GET /api/v1/health` | Status and configured provider mode |
| `GET /api/v1/providers` | Platform status, capabilities, honest integration message |
| `GET /api/v1/search?platform=tiktok&q=alex` | Normalized platform and candidate profile summaries |
| `GET /api/v1/profiles/{platform}/{username}` | Unified profile or explicit private state |
| `GET /api/v1/profiles/{platform}/{username}/posts?cursor=4` | Items and optional next cursor |
| `GET /api/v1/profiles/{platform}/{username}/stories` | Supported public story items |
| `GET /api/v1/profiles/{platform}/{username}/highlights` | Supported public highlight groups |
| `GET /api/v1/media/{platform}/{mediaId}/download` | Validated attachment, never an arbitrary-URL proxy |

Successful JSON: `{"data":{},"error":null}`. Failed JSON: `{"data":null,"error":{"code":"PROFILE_NOT_FOUND","message":"Profile was not found."}}`. Downloads return media bytes and `Content-Disposition: attachment`.

Missing profiles return 404; invalid input 400; private content 403; unsupported capabilities 422; unavailable providers 503; rate limits 429; provider timeouts 504. A private **profile** response is HTTP 200 with `accessStatus: "PRIVATE"` and the explicit public-only message; its content endpoints return 403. Unsupported counts are omitted rather than displayed as zero. Health is subject to the API rate limit.

```sh
curl -s 'http://localhost:8080/api/v1/search?platform=tiktok&q=%40alex.morgan'
curl -s 'http://localhost:8080/api/v1/profiles/instagram/alex.morgan/posts'
curl -s 'http://localhost:8080/api/v1/profiles/instagram/private.user'
curl -f -OJ 'http://localhost:8080/api/v1/media/instagram/post-1/download'
```

Input normalization accepts usernames, `@username`, HTTPS TikTok `/@username`, Instagram `/username/`, Facebook `/username`, and Facebook `/profile.php?id=digits` URLs on explicitly supported official hosts. A recognized URL determines the platform regardless of the selected browser value. Unsupported paths, credentials, arbitrary hosts, malformed URLs, non-HTTPS schemes, and internal IP/localhost inputs are rejected. Profile URL normalization does not make a network request.

## Providers and actual capabilities

Default `PROVIDER_MODE=mock`; “Demo” means exercised local fixtures only:

| Platform | Profile | Posts | Stories | Highlights | Download | Status |
| --- | --- | --- | --- | --- | --- | --- |
| TikTok | Demo | Demo | Demo image/video | Demo | Demo images | MOCK |
| Instagram | Demo | Demo | Demo image/video | Demo | Demo images | MOCK |
| Facebook | Demo | Demo | Demo image/video | Demo | Demo images | MOCK |

`PROVIDER_MODE=live`:

| Platform | Profile | Posts | Stories | Highlights | Download | Status |
| --- | --- | --- | --- | --- | --- | --- |
| TikTok | Unavailable | Unavailable | Unavailable | Unavailable | Unavailable | UNAVAILABLE |
| Instagram | Unavailable | Unavailable | Unavailable | Unavailable | Unavailable | UNAVAILABLE |
| Facebook | Unavailable | Unavailable | Unavailable | Unavailable | Unavailable | UNAVAILABLE |

Mock search is supported; live search is unavailable. The UI reads `/api/v1/providers`, displays supported tabs, and derives reels from returned `REEL` media rather than claiming a separate reels API. Capability truth comes from executable providers, not this table.

To add a provider, implement `SocialProvider` in its platform package, return unified models and safe typed errors, declare only supplied capabilities, honor context cancellation, and preserve public/private/unavailable semantics. Keep provider models internal. Resolve downloads by media ID with an owner and explicit trusted host list. Do not accept user-supplied fetch URLs or credentials. Add contract and genuine public integration tests, document mechanism/permissions/rate limits and captured results, then enable capabilities. `VERIFIED` requires successfully retrieving expected public information; an unavailable adapter test is insufficient.

TikTok's documented [Display API](https://developers.tiktok.com/docs/en/display-api-get-started) requires an authorized user's access token, so it does not fulfill credential-free arbitrary public-profile retrieval. Meta's [Instagram Platform](https://developers.facebook.com/docs/instagram-platform/) and [Page Public Content Access](https://developers.facebook.com/docs/features-reference/page-public-content-access/) documentation requests returned HTTP 429 during research; those restrictions were respected. No general permitted credential-free live integration was verified. Individual platform verification notes record these limits honestly.

## Privacy and security model

GhostView never requests platform passwords, cookies, tokens, or sessions. It implements no login, private API harvesting, CAPTCHA workaround, rate-limit bypass, or private-profile scraping. Demo mode makes no social platform requests. The story viewer has no platform view-registration mechanism. The operator and hosting/network infrastructure may still observe requests; “privately” is not a promise of anonymity from them. Future direct remote media could reveal viewer network information to its host and requires a separate privacy review.

Dynamic provider text uses text nodes, not provider HTML. CSP restricts scripts/styles/media/images/connections to the application; additional headers include `nosniff`, `no-referrer`, restricted permissions, frame protection, and same-origin resource policy. CORS allows only configured exact origins. Methods, queries, identifiers, request bodies (4 KiB), headers, and timeouts are constrained. Public error envelopes omit internal messages and credentials. JSON request logs contain method, status, and duration, not search terms, cookies, or tokens.

Download security is enforced independently: media IDs resolve through providers; owner public access is checked; remote URLs require HTTPS and a provider-owned domain allowlist. DNS results must all be public addresses, and validated IPs are used for dialing. Loopback, RFC1918, link-local, metadata, special/reserved ranges, and unsafe redirects are blocked. Each redirect is revalidated; time, size, declared MIME type, and sniffed content are checked. Filenames are sanitized. Remote bodies are buffered to validate the complete bounded response before serving; memory use grows with concurrent downloads. Trusted mock SVG bytes are generated locally and are not an exception for arbitrary remote SVG.

Rate limiting and cache are bounded but process-local. `X-Forwarded-For` is ignored; behind a reverse proxy all clients may share its bucket. Use a trusted edge limiter/shared cache for multiple replicas or a public deployment. Do not trust arbitrary forwarding headers. External downloads share a connection pool; each initial request and redirect independently validates its provider allowlist and DNS. Newly opened sockets dial validated public IPs. No live integration currently consumes the pool.

Known limitations: no live platform data, no persistence/accounts, no mock video downloads, fixed demo timestamps/counts, and no Redis/distributed limits. In-memory profile status can remain cached for up to five minutes; a live provider must account for privacy changes before release. Public visibility does not establish permission to redistribute or download content; any future integration must enforce its actual permissions and licensing terms.
