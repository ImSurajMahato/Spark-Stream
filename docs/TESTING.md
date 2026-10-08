# Verification record

Initial preparation: 2026-10-07. Upstream pinned to ba7c6f60b8eed6120ffcebe85322b81205712004.

- gofmt completed for modified Go files.
- go test -race ./internal/security passed.
- Full application build/test status will be recorded here after execution finishes.
- No real Telegram token, channel, video, deployment or payment used.
- No 512MB memory/load/playback benchmark. No dependency audit.
- No visual player was added: use your existing website player.

Local results:
- config package compiled and security tests passed.
- Full app test compilation was not completed: generated github.com/gotd/td/tg compilation was killed by workspace memory limits; limited-duration retry with lower GC did not finish. This is not a passing build. CI must finish before production use.

## October 8 custom-slug changes

Added MongoDB v2 driver, permanent string slug records, owner /setslug, upload-generated random IDs and public /start lookup. TTL fixed to 14400 seconds. gofmt done. Race tests for security and catalog PASS, config compiled. Full go test ./... timed out while compiling the Telegram dependencies in the roughly 2GB workspace, so command integration/full application is NOT build-verified. No live MongoDB roundtrip/duplicate race, Telegram deep-link/reply, MX Player/VLC playback, Docker build or 512MB benchmark performed.

Before deployment: full build/vet/tests on adequate RAM; connect a test secured MongoDB; check random/manual slug, idempotent retry, duplicate-to-other-file rejection, unavailable DB, invalid slug and unauthorized /setslug/upload; /start from a non-owner; player start/seek; four-hour new-request expiry and fresh link after expiry. Existing connections may continue. Back up catalog and channel copies.
