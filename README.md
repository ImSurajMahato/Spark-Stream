# Spark Stream

Telegram lecture streaming in Go. Private source repository, not a private-media paywall by itself.
Based on [EverythingSuckz/TG-FileStreamBot](https://github.com/EverythingSuckz/TG-FileStreamBot), AGPL-3.0. See LICENSE, NOTICE and docs/UPSTREAM-README.md.

## Kya bana hai

- Telegram se bytes RAM chunks mein aate hain, poora lecture server disk pe download nahi hota.
- HTTP Range/seek, correct 206 headers, HEAD and invalid-range 416 support.
- First block alone fetch hota hai; first 32KB response flush. After that parallel bounded prefetch.
- At most 256KB per chunk, default 2 fetches + 4 queued chunks per request.
- HMAC-SHA256 signed links, 32-character file hash, default 6-hour expiry. Missing/bad/expired signature is rejected BEFORE Telegram lookup.
- Required uploader allowlist, safe filenames, nosniff headers, unsafe document types forced to download.
- Per-IP request rate limit with bounded map; global active stream cap, default 8 requests (not 8 guaranteed viewers).
- Optional backend-only link mint API to refresh playback URLs without uploading a lecture again.
- No student catalog, login, billing, DRM, transcoding or HLS ABR conversion is included.

**Zero buffering promise nahi hai.** Telegram response, host bandwidth, student internet and video bitrate matter. This is a lower-startup-delay implementation, not a measured zero-buffer service. 512MB capacity has NOT been load tested; previous 20-30 viewer estimates should not be treated as guaranteed.

## Setup (Docker)

Build on a machine with more RAM, or CI, not a 512MB server. Go's Telegram generated code can need substantial build RAM. Runtime target and build requirement are different.

1. Telegram API ID/hash from https://my.telegram.org, bot token from BotFather, private log channel. Add your bot as channel admin with posting rights. Save the channel ID and YOUR numeric Telegram user ID. Never commit secrets.
2. Clone/download this private repo, then:

```sh
cp fsb.sample.env fsb.env
chmod 600 fsb.env
openssl rand -hex 32
```

3. Edit fsb.env: API_ID, API_HASH, BOT_TOKEN, LOG_CHANNEL, ALLOWED_USERS, HOST and SIGNING_SECRET. Paste the generated random value only into SIGNING_SECRET locally. Example ALLOWED_USERS=your_numeric_ID, no username or placeholder text. HOST is your HTTPS streaming domain with no trailing slash. PORT=8080 with this compose file.
4. Start:

```sh
docker compose build
docker compose up -d
docker compose logs --tail=60 spark
```

Compose builds THIS source, not the old upstream image. It exposes only localhost:8080: put your existing HTTPS reverse proxy in front. The session volume survives restarts. Never expose plain HTTP or the mint key publicly. On hosted platforms without compose, build the included Dockerfile, set the same environment variables and their required PORT, add persistent writable /app/data if supported. Ephemeral sessions cause repeat logins.

5. Send one MP4 lecture to your bot from an allowed Telegram ID. It forwards to the configured log channel and returns an expiring streaming URL. Keep the channel copy: deleting it breaks playback. Use URL directly in your EXISTING website's video player.
6. Test start, seeking, pause/resume, several viewers, and disconnects before adding the full library. Do not assume Docker memory limits alone make the service fast.

## Website integration and link refresh

An expiring URL is not a permanent catalog URL. Store the Telegram message ID in your website database along with title/course/lesson. Website BACKEND must verify student's login/enrollment before requesting a new link. Never send MINT_API_KEY or SIGNING_SECRET to frontend JavaScript.

Generate a DIFFERENT random secret and set MINT_API_KEY (blank disables the endpoint). From trusted backend:

```sh
# Environment variables below belong on the backend, never in browser JS.
curl -X POST "$STREAM_HOST/api/link/$MESSAGE_ID" \
  -H "Authorization: Bearer $MINT_API_KEY"
```

Response: JSON with url and expires. Set video.src to url. Request fresh URL before expiry, or retry once with a refreshed URL on a 403 and restore video.currentTime. No automated player refresh implementation is included. Existing connections may finish after link expiry; new range requests cannot. A valid URL is shareable until expiry; this is not DRM or per-student binding. Rotating SIGNING_SECRET invalidates all old URLs.

Set Referrer-Policy: no-referrer on your website; do not log signed query strings in proxies/analytics. Server logs paths/status only, not query credentials. Put streaming on a separate origin. Cross-origin browser video.src normally works; fetch/canvas access needs deliberately configured CORS, not wildcard credential CORS.

## 5,000-10,000 lectures

Count is not simultaneous playback load. The bot uses one LOG_CHANNEL; this version has no multi-channel catalog. IDs can reference 10,000 lectures without keeping their video bytes in RAM; metadata cache is bounded to 10MiB. Your site database owns search/categories. Server still relays EVERY viewer's bytes and needs enough egress. A free 512MB host can sleep or cap bandwidth; do not promise public teaching uptime from that alone.

Store independent backups. Upload only media you own/have permission to use. No guarantees about Telegram permanent storage, bans, quotas or upload speed. Respect Telegram flood waits; don't evade platform limits with channels/accounts. Start with a small library trial. This repo does not include a bulk uploader.

For browser compatibility use MP4 with H.264/AAC. If MP4 moov metadata is at the end, move it without re-encoding on your own computer:

```sh
ffmpeg -i lecture.mp4 -c copy -movflags +faststart lecture-faststart.mp4
```

This is a local preparation command, not server transcoding. Changing resolution/bitrate needs re-encoding; no fixed file-size reduction is guaranteed.

## 512MB tuning and trial

Defaults: STREAM_CONCURRENCY=2, STREAM_BUFFER_COUNT=4, MAX_ACTIVE_STREAMS=8, GOMEMLIMIT=350MiB. GOMEMLIMIT is a soft Go target, not a hard RAM guarantee. Slots cap active HTTP requests; browsers may make multiple ranges. A full system's RAM includes Telegram clients, SQLite, metadata cache, Go runtime, logs and host overhead. Chunk buffers alone are not whole-process memory.

Measure docker stats plus browser time-to-first-frame, stalls, seek latency and host outbound bytes. Start 1 viewer, then 2, 4, 8; also test slow clients/disconnects for 30 minutes. If memory rises or playback stalls, reduce active requests and bitrate, or upgrade bandwidth/host. 429 is request limit; 503 is active-stream cap; 403 means expired/invalid link; 416 means invalid range; 502 means upstream/startup failure. Limits reject excess demand rather than pretending everyone can play.

Behind a proxy the default limiter sees the proxy IP, so all viewers may share one bucket. Do not trust arbitrary X-Forwarded-For: only configure trusted proxy IPs after you know the host topology. If no trusted proxy is configured, tune rate limits for aggregate proxy traffic. Mint API must also be protected by backend network restrictions where possible.

## Test and limits

```sh
go test -race ./internal/security
go test -p 1 ./...
go vet -p 1 ./...
go build -p 1 -o spark-stream ./cmd/fsb
```

Security tests cover HMAC tampering/expiry, ranges, safe headers and rate limiter bounds/concurrency. CI runs full compile/test/vet. No real Telegram playback, Docker deployment, 512MB load test or third-party dependency security audit was performed during initial preparation. CDN redirects are still unsupported upstream; some Telegram files may fail. Upstream file-reference refresh after expiry is not implemented; stale metadata can need retry/restart. See docs/TESTING.md for current local verification status.

## License

AGPL-3.0 inherited from upstream. Preserve attribution. A private repo does not remove network-use source obligations: offer corresponding source to users who interact with your modified service as required by the license. This is not legal advice. Keep secrets out of source offers.
