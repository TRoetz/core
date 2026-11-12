# Known bugs found by the E2E suite

Entries are appended by `internal/e2e` tests that hit a product bug: the
affected subtest is skipped with `t.Skip("BUG: ...")` and one bullet lands here.
Format: `- <area>/<UC>: <one-line symptom> — evidence: <doc line + observed response>`
