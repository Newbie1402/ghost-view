# Facebook provider verification

- Live status: **UNAVAILABLE**. Default demo status: **MOCK** through the shared fixture provider.
- Last investigated/tested: **2026-10-06**.
- Mechanism: live adapter returns safe UNAVAILABLE errors; no network fetch or scraping.
- Supported live operations: none. Search/profile/posts/stories/highlights/download capabilities are all false.
- Unsupported live operations: all requested content operations, including arbitrary public-profile discovery.
- Authentication research: Meta's [official oEmbed plugin](https://github.com/facebook/meta-embeds-for-wordpress/blob/main/README.md) confirms tokenless post/reel embeds; it does not establish a Facebook profile-search API. GhostView accepts no user credentials.
- Candidate endpoints: `GET https://graph.facebook.com/v25.0/oembed_post?url=<validated-post-url>` and `/v25.0/oembed_video?url=<validated-reel-url>`. Use `Accept: application/json`; no Cookie or Authorization. These endpoints were not claimed as working profile providers.
- Research: direct [Facebook oEmbed Post docs](https://developers.facebook.com/docs/graph-api/reference/oembed-post/) returned HTTP 429 on 2026-10-06. No retry/bypass followed. The official repository was inspected instead; no third-party mirror was relied upon.
- Rate limits: upstream quota unknown; documentation could not be inspected. Application default: 120 API requests/minute/client IP.
- Live investigation input: `https://www.facebook.com/bacbeodangiuu`. `alex.morgan` exercises MOCK fixtures only; unavailable adapter contracts use `alex`.
- Normalized URL forms: `/username` and `/profile.php?id=digits` on supported Facebook hosts; URL acceptance is not evidence of live retrieval.

Actual local contract output, executed 2026-10-06:

```text
go test -v ./internal/provider/... -count=1
=== RUN   TestRealProviderContracts/facebook
    --- PASS: TestRealProviderContracts/facebook (0.00s)
```

The Facebook subtest checked platform identity, UNAVAILABLE status, false capabilities, and UNAVAILABLE errors from every operation in a passing local provider-suite run. It did not retrieve public information. Live probe evidence is tracked in [REAL_PROVIDER_REPORT.md](../REAL_PROVIDER_REPORT.md); do not mark VERIFIED until actual retrieval tests pass. Mock behavior and captured output: [mock.md](mock.md).

Additional anonymous `GET https://www.facebook.com/bacbeodangiuu` on 2026-10-06 returned HTTP 200, text/html, 624103 bytes with Open Graph name Hoàng Kim Bạc and `profile_header_renderer`, but no explicit public privacy flag or feed/media nodes could be established (`username_for_profile=null`). Headers were `User-Agent: GhostView/1.0 (+public-content verification)` and `Accept: text/html`, with no Cookie/Authorization or cookie jar. HTTP 200 and a name are insufficient to mark a profile PUBLIC; the implementation remains UNAVAILABLE. Platform Set-Cookie values were not retained or reused.
