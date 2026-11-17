# Synthetic development performance baseline

Recorded 17 November 2026 on the shared native development host: Go 1.27.4, PostgreSQL 18.3, API schema 6, loopback HTTP, synthetic records only. Other verification/build work could overlap; this is not a dedicated-host or production-size benchmark.

The pinned k6 2.4.0 script ran two login probes, 20 registrations across two virtual users, and five seconds of search across two virtual users (90 successful searches). It recorded zero failed HTTP requests. The tiny dataset and low authentication sample count limit the conclusion; authentication probes deliberately stay within the durable ten-attempt-per-IP/account window rather than disabling the control.

| Operation | Average (ms) | p95 (ms) | Maximum (ms) |
| --- | ---: | ---: | ---: |
| Login probes | 122.030 | 147.244 | 150.046 |
| Patient search | 11.972 | 18.287 | 33.257 |
| Registration | 10.327 | 21.601 | 29.786 |

These are observed local values, not acceptance targets or clinical capacity claims. Repeat against representative synthetic volume, an isolated target and agreed concurrency/SLOs before performance acceptance. Search audit hits are batched to avoid per-result write round trips; this does not establish large-dataset search-index adequacy.

Reproduce with a fresh/rate-limit-compatible synthetic development session:

```bash
python3 scripts/install-security-tools.py deploy/performance-tools.json
set -a; source .env; set +a
.local/tools/k6 run --quiet tests/performance/baseline.js
.local/tools/k6 run --quiet tests/performance/report_privacy.js
```

The benchmark creates synthetic registry records and uses the receptionist account; it never bypasses authentication rate limits. Do not repeatedly rerun within the same authentication window and interpret 429 as successful login performance. The report explicitly allowlists aggregate metrics/state/checks and excludes k6 setup data and options, which can contain credentials. Only the sanitized aggregate report is retained under ignored .local/performance.json. No tokens, patient bodies or actual identifiers are published.
