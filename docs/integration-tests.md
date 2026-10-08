# Elasticsearch integration tests

The default go test ./... run skips the live TestESClient test. An endpoint
alone never enables network access. Retained regressions use disposable loopback
HTTP servers and synthetic credentials.

Run a live read/scroll test only against a service and index you are authorized
to access, with explicit environment opt-in:

    ZIPKIN_ES_INTEGRATION=1 \
    ZIPKIN_ES_URL=https://your-approved-elasticsearch-endpoint \
    ZIPKIN_ES_INDEX=your-approved-index \
    ZIPKIN_ES_SERVICE=your-approved-service \
    go test -run '^TestESClient$' -count=1 .

ZIPKIN_ES_URL must be an absolute HTTP or HTTPS URL without embedded credentials,
query parameters or fragments. For Basic Auth, supply ZIPKIN_ES_USERNAME and
ZIPKIN_ES_PASSWORD together through the existing approved environment/secret
mechanism. Never place credential values in source, command history, test output,
or issue/PR descriptions.

The test confines authenticated requests to that origin, uses a 10-second HTTP
timeout and a 30-second overall deadline, and waits for its own scroll cleanup.
Configuration errors and this test's failure messages omit supplied values;
request debug logging is disabled.

Removing the old literal from current code does not erase Git history or revoke
any credential. Its current validity was not checked.

The retained json-iterator v1.1.6 / reflect2 v1.0.1 dependency combination
crashes during the client's map encoding on Go 1.18.10 and Go 1.26.9 in isolated
synthetic scroll trials. This credential-removal change does not repair that
existing runtime compatibility issue. The default-run and opt-in regressions
check the real test's gate, authenticated ping and failure-output redaction;
transport checks cover authentication confinement and cleanup signaling.
A successful full live scroll is not claimed.
