# Real provider investigation

Date: **2026-10-06**. This report separates official integration evidence, executed retrieval, and reference-site advertising. Mock contracts validate product behavior; they do not satisfy real-data verification.

## Official interfaces

| Platform | Public candidate | Authentication | What is established |
| --- | --- | --- | --- |
| TikTok | Creator `/oembed?url=<profile-url>` | No credentials documented | Official profile metadata/embed endpoint; raw collections/downloads not supplied in documented JSON |
| Instagram | Graph v25 `/instagram_oembed?url=<profile-or-post-url>` | Tokenless in Meta's official plugin | Profile/post/reel embed support; returned profile fields need executable verification |
| Facebook | Graph v25 `/oembed_post` and `/oembed_video` | Tokenless in Meta's official plugin | Content permalink embeds; general profile discovery not established |

Use GET requests to fixed official HTTPS endpoints, URL-encode validated platform links, apply context/client deadlines, and request JSON. An identifiable GhostView User-Agent is appropriate. No Cookie, Authorization, fake browser session, CAPTCHA mechanism, credential, or user token is required for these candidate requests. Required custom headers are not documented in the sources inspected. Quotas are unverified; upstream 403/429/login/challenge responses must terminate retrieval and produce an honest unavailable/rate-limit state.

TikTok [creator-profile documentation](https://developers.tiktok.com/docs/en/embed-creator-profiles) and [video oEmbed documentation](https://developers.tiktok.com/docs/en/embed-videos) were accessible. Their sample responses distinguish profile metadata from a single known video's caption/thumbnail. Neither establishes arbitrary username post enumeration, stories, highlights, or raw download access. TikTok [Display API](https://developers.tiktok.com/docs/en/display-api-get-started) requires approved products, owner authorization, and scopes; it is outside the requested anonymous arbitrary-profile flow.

Meta's [official WordPress implementation](https://github.com/facebook/meta-embeds-for-wordpress/blob/main/includes/class-meta-embeds.php) independently confirms the fixed Graph v25 endpoints and supported link patterns without token configuration. Its model is third-party embed rendering. GhostView must not render returned provider HTML or automatically load platform SDKs, since the requested app forbids untrusted HTML and promises no deliberate story-view registration. Technical embed responses alone do not establish permission for media downloading.

Direct Meta docs attempted once per relevant URL: Instagram oEmbed and Facebook oEmbed Post returned HTTP 429; plugins/oEmbed and a tokenless announcement URL were inaccessible to the web tool. The rate restrictions were respected. Third-party mirrors and blogs returned by search were excluded from technical evidence.

## Executed retrieval evidence

Root executed ordinary anonymous Go HTTP requests using an identifiable application User-Agent, HTML Accept headers, no CookieJar/Cookie/Authorization, and no saved or reused platform Set-Cookie values. Results on 2026-10-06:

| Probe | Actual result | Consequence |
| --- | --- | --- |
| TikTok public `/@marcmarquez93` | HTTP 200, 399097 HTML bytes; bootstrap status 0 and `privateAccount=false`; nickname Marc Márquez, 3100000 followers, 35 following, 349 videos; empty `itemList` | Implement exact lookup/profile metadata only |
| TikTok documented creator oEmbed | HTTP 429, 19 bytes, text/plain; no retry | Do not treat documented availability as successful execution |
| Instagram public `/marcmarquez93/` | HTTP 200, 862382 HTML bytes; explicit `is_private=false`; 8424528 followers, 455 following; 12 timeline edges: 10 carousel, 2 video | Implement public metadata and available photo/carousel-cover previews only |
| Instagram first embedded display-image URL | HTTP 200, image/jpeg, 51360 bytes; JPEG magic asserted | Real photo retrieval executed; not a claim of download permission |
| Instagram official Graph v25 profile oEmbed | HTTP 400 JSON, code 100, OAuthException, Invalid parameter | Public HTML parser is the selected mechanism; profile oEmbed was not verified despite repository pattern |
| Facebook `/bacbeodangiuu` | Initial root probe HTTP 200, 624263 HTML bytes; subsequent Go probe 624103 bytes, Hoàng Kim Bạc header identity, no established explicit privacy field or media nodes | Fail closed UNAVAILABLE; identity alone is insufficient |

TikTok's clean anonymous browser independently loaded a public profile with empty post items. The browser's native SDK post-list request returned HTTP 200 with an empty body and dynamic signing/session query requirements. GhostView does not generate or emulate those fields, reuse sessions, or convert this failure into a fake posts capability.

The TikTok/Instagram adapters are **EXPERIMENTAL** while API/UI end-to-end checks are pending. Public HTML JSON can change at any time and is not an official stable API model. Parser tests exercise explicit private handling and fail-closed states locally; they do not establish a real private-account access claim. Successful root probes above establish only the listed public data. Runtime capability flags must remain limited to those operations.

Executed local provider-suite rerun on 2026-10-06 after correcting an unsupported-download error mismatch:

```text
go test -v ./internal/provider/... -count=1
--- PASS: TestRealProviderContracts (0.00s)
    --- PASS: TestRealProviderContracts/tiktok (0.00s)
    --- PASS: TestRealProviderContracts/instagram (0.00s)
    --- PASS: TestRealProviderContracts/facebook (0.00s)
ok ghostview/internal/provider 0.434s
ok ghostview/internal/provider/instagram 0.302s
ok ghostview/internal/provider/mock 0.164s
ok ghostview/internal/provider/tiktok 0.572s
```

Opt-in external-network tests were skipped in this local run. They must be executed separately; no skipped case counts as successful live retrieval. Service-level profile data currently has a five-minute TTL, so visibility changes can be stale even when the provider's own cache is shorter. A live release must disclose/handle this limit rather than promise instantaneous privacy-state freshness.

The Instagram implementation agent subsequently executed the real network assertion:

```text
GHOSTVIEW_LIVE_TEST=1 go test ./internal/provider/instagram -run TestLivePublicProfileAndImage -count=1 -v
username=marcmarquez93 access=PUBLIC photoPreviews=9 CDNStatus=200 imageBytes=51360
--- PASS: TestLivePublicProfileAndImage (1.17s)
```

This confirms the public profile and at least one real JPEG preview; it does not test videos, stories, highlights, downloads, or the browser UI. The upstream count is dynamic (the initial probe had ten carousel/image entries). Items are marked `previewOnly: true`; no full carousel or fabricated pagination is implied. `all_media_count` was null and is omitted. Signed CDN image links were used transiently without printing or persisting them.

## Reference behavior

| Reference | Observed | Advertised but not execution-verified |
| --- | --- | --- |
| [Mollygram Vietnamese page](https://mollygram.com/vi) | Public landing page fetched; username/link input, View action, resource links, loading label | Public Instagram stories/posts/highlights/reels and downloads, private-account exclusion, no-login/anonymity claims |
| [TTViewer Vietnamese page](https://ttviewer.net/vi) | Root reported web read HTTP 403; no bypass attempted | Functionality cannot be independently established from a blocked response |

Mollygram's page text is evidence of its advertised UX, not proof that retrieval succeeds or that its internal technique is permitted. A search result/profile/media workflow must actually execute before being described as verified. TTViewer's access restriction is not evidence of private content. Browser observations, if available, belong in this report separately from text advertising.

Current GhostView comparison: local fixtures exercise the full advertised-style product interactions; real TikTok supplies profile metadata; real Instagram supplies profile metadata and available first-page photo previews. Neither currently matches the reference's advertised live stories/highlights/playable-video/download scope. Mock acceptance passing cannot close this gap.

Neither reference is a GhostView upstream provider. No undocumented third-party endpoint, captured credentials, cookie/session values, or platform restriction bypass has been introduced. Similar interface behavior does not justify copying an unverified backend mechanism.

## Release criterion

A provider may advertise only fields/operations returned by its real implementation. A successful endpoint health check, retrieved HTML shell, or canned oEmbed sample is insufficient. Verify a known public input through Go, validate response shape and public availability, run contract tests, and record sanitized actual output. Otherwise retain EXPERIMENTAL/UNAVAILABLE and explicit unsupported operations. Do not label all original product goals complete while only metadata retrieval is available.
