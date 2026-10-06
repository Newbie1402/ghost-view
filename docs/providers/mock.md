# Mock provider verification

- Status: **MOCK**, never VERIFIED real platform access.
- Last verified: **2026-10-06**.
- Mechanism: deterministic Go data and local SVG/MP4 assets; no social network calls.
- Supported: exact username and ambiguous display-name search, profiles, two-page posts (four items each), reels, image/video stories, highlights, generated SVG image downloads.
- Unsupported: live data, video downloads, arbitrary profiles, platform view registration.
- Authentication: none. Never supply platform credentials.
- Rate limits: application default 120 API requests/minute/client IP; no upstream provider quota.
- Inputs: `alex.morgan` public, `alex` / `Alex Morgan` ambiguous, `private.user` private, `nobody` empty. `unavailable`, `rate.limited`, `timeout` model failure states.
- Fixture dates are fixed; stories are demo data and do not imply current platform availability.

Executed command and actual output on 2026-10-06:

```text
go test -v ./internal/provider/... -run 'Test(MockProviderContract|RealProviderContractsUnavailable)' -count=1
=== RUN   TestMockProviderContract
=== RUN   TestMockProviderContract/tiktok
=== RUN   TestMockProviderContract/instagram
=== RUN   TestMockProviderContract/facebook
--- PASS: TestMockProviderContract (0.00s)
    --- PASS: TestMockProviderContract/tiktok (0.00s)
    --- PASS: TestMockProviderContract/instagram (0.00s)
    --- PASS: TestMockProviderContract/facebook (0.00s)
PASS
ok  ghostview/internal/provider/mock 0.297s
```

The contract executed search cardinality, public/private/missing profiles, pagination, image/video stories, highlights, and downloadable/unavailable media. It validates local product behavior only.
