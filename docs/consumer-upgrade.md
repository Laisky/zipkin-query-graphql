# Consumer dependency compatibility

This application keeps the existing github.com/Laisky/go-utils module path and selects its final compatible v1 release, v1.17.1. Migration to the v6 module path is separate API work.

The removed go-utils/gin-middlewares package is migrated to the extracted github.com/Laisky/gin-middlewares v1.3.0 API. Its handler adapter preserves Gin context, and auth initialization continues to use the configured signing secret and refuses an empty secret at startup. Synthetic valid, wrong-key, expired and missing-cookie cases protect this boundary. Route authorization policy is unchanged.

The Gin 1.9 dependency change is reconciled onto the credential-clean master. Gin 1.9.1 contains the patched attachment-header behavior. Current x/net fixes require v0.60.0 and Go 1.26; module selection also brings patched crypto/text versions. This raises the previous Go 1.12 module floor. Validation and the fast CI gate use the patched Go 1.27.2 compiler.

Removed startup APIs are migrated to CreateNewDefaultLogger, SetInternalClock and a consumer-local single-file YAML loader, preserving accepted log levels, the 100 ms refresh and single-file loading. The replacement utility loader follows include fields even when disabled; a synthetic missing-include fixture protects the legacy behavior.

The source scan identified a reachable denial-of-service advisory in the legacy gqlparser module, which has no fixed v1 release. gqlgen is upgraded to v0.17.95 with gqlparser/v2 v2.5.62 and the executable schema is regenerated. The schema, existing model definitions and trace resolver implementation are preserved. The handler uses the successor gqlgen server API and a narrow adapter preserves JSON POSTs with missing or form Content-Type headers without mutating the caller request. The synthetic trace query verifies the Date scalar, default environment, response fields and real resolver without an external Elasticsearch service.

Synthetic regressions cover startup configuration, JSON request maps, the actual GraphQL/Gin adapter, all scroll pages and rotating IDs, exact span limits, and successful ping-body closure. No Elasticsearch credentials or external endpoint are needed. The live integration test remains disabled unless its explicit opt-in configuration is supplied.

All acceptance is recorded against an exact current-base candidate. The Docker builder uses the verified patched Go 1.27.2 Alpine 3.23 image; the existing runtime stage is preserved. Actual image qualification found that extldflags alone did not force static linkage and Alpine lacked the Asia/Shanghai timezone data required during go-utils initialization. The build now requests external static linking and the standard timetzdata tag, embedding timezone data without adding a runtime package. No image is pushed or deployed.

Imported Logrus and Prometheus dependencies are raised to their reported fixed versions. Their baseline advisories had no called symbols; this does not imply the application had a reproduced exploit. The unimported, unmaintained x/crypto/openpgp module advisory has no fix and remains recorded separately.

Prior Snyk failure is an external status with unavailable underlying findings; no specific cause is inferred.
