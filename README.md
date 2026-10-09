# Spark Stream

Telegram lecture streaming in Go. Private source repository, not a private-media paywall by itself.
Based on [EverythingSuckz/TG-FileStreamBot](https://github.com/EverythingSuckz/TG-FileStreamBot), AGPL-3.0. See LICENSE, NOTICE and docs/UPSTREAM-README.md.

## Kya bana hai

- Telegram se bytes RAM chunks mein aate hain, poora lecture server disk pe download nahi hota.
- HTTP Range/seek, correct 206 headers, HEAD and invalid-range 416 support.
- First block alone fetch hota hai; first 32KB response flush. After that parallel bounded prefetch.
- At most 256KB per chunk, default 2 fetches + 4 queued chunks per request.
- HMAC-SHA256 signed links, 32-character file hash, fixed 4-hour expiry. Missing/bad/expired signature is rejected BEFORE Telegram lookup.
- Required uploader allowlist, safe filenames, nosniff headers, unsafe document types forced to download.
- Per-IP request rate limit with bounded map; global active stream cap, default 8 requests (not 8 guaranteed viewers).
- Optional backend-only link mint API to refresh playback URLs without uploading a lecture again.
- No student login, enrollment, billing, DRM, transcoding or HLS ABR conversion is included.

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

3. Edit fsb.env: API_ID, API_HASH, BOT_TOKEN, LOG_CHANNEL, ALLOWED_USERS, HOST and SIGNING_SECRET. Paste the generated random value only into SIGNING_SECRET locally. ALLOWED_USERS=7513979260 is prefilled for Suraj (the numeric ID he supplied). The same ID is the default when the variable is absent. Set your own IDs before reusing this source for another owner; no username or placeholder text. HOST is your HTTPS streaming domain with no trailing slash. PORT=8080 with this compose file.
4. Start:

```sh
docker compose build
docker compose up -d
docker compose logs --tail=60 spark
```

Compose builds THIS source, not the old upstream image. It exposes only localhost:8080: put your existing HTTPS reverse proxy in front. The session volume survives restarts. Never expose plain HTTP or the mint key publicly. On hosted platforms without compose, build the included Dockerfile, set the same environment variables and their required PORT, add persistent writable /app/data if supported. Ephemeral sessions cause repeat logins.

5. Send one MP4 lecture to your bot from an allowed Telegram ID. It saves to the configured log channel and asks for a custom slug. Enter it, or /skip for a random ID. The bot returns a permanent lecture link with stream/file choices. Keep the channel copy: deleting it breaks playback. Use URL directly in your EXISTING website's video player.
6. Test start, seeking, pause/resume, several viewers, and disconnects before adding the full library. Do not assume Docker memory limits alone make the service fast.

## Permanent custom ID + MongoDB + bot link

Set MONGODB_URI privately in fsb.env and MONGODB_DATABASE=spark_stream. MongoDB must be a separate secured service, not another process squeezed into the 512MB streaming container. No database is provisioned by this repo. Require authentication/TLS, restrict network access and back up the catalog. The bot stops at startup if the database is unavailable. SQLite still holds Telegram session/peer state; MongoDB holds lecture mappings, not video bytes.

1. Upload ONE lecture as the owner (7513979260). Bot saves it in LOG_CHANNEL, creates a random 24-hex fallback ID and asks you to enter a custom slug. Reply with only your slug, for example `6a2698f9735cb5428a449a0a`. No separate /setslug command needed. Use `/skip` for the random ID; `/cancel` also keeps the random link and closes the prompt (does NOT delete the file).

The bot waits for this file to finish before accepting another file. A second upload is NOT forwarded while a slug is pending: resend it after finishing the first. Pending state is stored in MongoDB, so a restart does not lose the prompt. Duplicate/invalid slugs keep the current file pending; try another ID. Random fallback remains usable even after selecting a custom alias. Deploy only ONE bot process/replica: per-owner request serialization is process-local, not a distributed upload queue. This is not a bulk/album uploader. Database errors fail closed; if saving a prompt/catalog fails after forwarding, the reply provides a fallback link or log message ID for /setslug recovery. Telegram/network uncertain sends can still need checking the log channel before retrying.
2. /setslug remains an optional OWNER recovery/extra-alias command:

```text
/setslug 6a2698f9735cb5428a449a0a 123
```

123 means the actual message ID in your configured log channel, NOT the ID from your original private chat. The bot checks the media, computes its hash and inserts the mapping. Allowed IDs: 1-64 ASCII letters, digits, underscore or hyphen. IDs are case-sensitive. Random IDs use crypto/rand. A duplicate ID for another lecture is rejected, never silently overwritten. Same-file retries are idempotent. You can create multiple aliases for one lecture. No delete/rebind command is provided.

3. Share the returned `https://t.me/YOUR_REAL_BOT_USERNAME?start=6a2698f9735cb5428a449a0a`. YOUR_REAL_BOT_USERNAME is your deployed BotFather bot, not the example @streambot. The first visit may require pressing Start in Telegram. The bot resolves /start ID from MongoDB and shows two inline buttons: **Get Stream Link** and **Get File**. Get Stream Link generates a fresh signed HTTPS URL when clicked. Get File copies the actual media from LOG_CHANNEL into the student's private chat using Telegram server-side delivery, without downloading the full file into this host. Captions and forward attribution are stripped. Protected-content restrictions and Telegram flood limits are respected; failure returns an error, not a bypass. Deleted/changed source media fails closed.
4. Student copies the COMPLETE URL including hash/expires/sig into MX Player's Network stream or VLC's Open network stream. This returns a URL, not an automatic app-launch guarantee. MP4 H.264/AAC is the practical trial format. Actual MX/VLC playback is not verified yet.

**4 hours kya expire hota hai?** Only the generated stream URL, counted from issuance, not the permanent custom ID, original log-channel file or lecture catalog row. LINK_TTL_SECONDS must be 14400. Each Get Stream Link button click issues a URL with a new expiry; requests in the same second can return the same URL. At exactly expiry new GET/HEAD/seek/reconnect requests get 403. A connection opened before expiry can continue; it is not forcibly cut off at the four-hour mark. Open the same permanent bot link to obtain another four-hour URL.

MongoDB `pending_uploads` stores one unfinished prompt per owner, including its random fallback entry and original upload message ID. `/skip`, `/cancel` or a successful custom slug clears it. These records have no automatic expiry. MongoDB collection `lectures` stores `_id` as a STRING slug, `channel_id` (normalized positive configured channel ID), `message_id`, `hash` (32 hex) and `created_at`. There is NO TTL index. Do not store a 24-hex slug as BSON ObjectId: it must remain a string. The unique _id prevents races/rebinding. Preserve the Telegram channel copy. Changing LOG_CHANNEL requires a migration; mismatched records fail closed.

Anyone who has the permanent slug can ask the bot for a fresh link. ALLOWED_USERS restricts uploads, slug prompts and /setslug only, not student /start or the buttons. Slugs are identifiers, NOT an enrollment check. Anyone with a valid stream URL can share/use it until expiry. No one-time use, per-user binding, DRM or paid-student authorization is implied. Add explicit access checks before public use if your lectures require restricted enrollment.

**Get File: protected copy + four-hour auto-delete.** Each click saves a delivery intent in MongoDB BEFORE sending a server-side copy with Telegram `noforwards=true` (normal forwarding/saving disabled). Deadline is four hours from the delivery request; successful delivery is normally seconds later. Only the student's delivered message is deleted with revoke=true, NEVER the original log-channel lecture or permanent slug. Student can click again for a new copy; this is not one-time access.

`file_deliveries` holds intents, copied message IDs, deadlines and retries. It has a query index, NOT a TTL index: deleting a MongoDB row does not delete a Telegram message. Sweeper starts immediately on startup and every minute (50 jobs/batch), retries with capped backoff and respects Telegram flood waits. Run ONE bot instance, keep MongoDB and session storage persistent, and keep the host awake. A sleeping/offline bot deletes late on restart, not exactly at four hours. Large backlogs can also be late.

Crash between send and saving message ID: recovery retries the same Telegram random_id to reduce duplicate-send risk. Telegram deduplication is not a cross-system transaction or an unlimited guarantee. If the sent ID cannot be recovered before its deadline, the job becomes `failed` for owner manual review, rather than sending a fresh file after expiry. Telegram refusal/blocked user/late 48-hour cases also retain failed jobs, logged only by job ID. Inspect `file_deliveries` where state=failed. Never expose access_hash or connection strings. Successful deletion removes its job. Already-deleted messages may return an error or idempotent success.

Protection is Telegram client-level, NOT DRM. Screenshots/screen recording, cameras, modified clients or copies already extracted cannot be reliably blocked or erased. It does not protect MX/VLC HTTP stream URLs against downloading/sharing. Both buttons remain public to anyone holding a slug. Existing files delivered before this update are not retroactively scheduled or protected.

Approach checked against https://github.com/CodeXBotz/File-Sharing-Bot/blob/main/plugins/start.py (copy + protect_content + auto-delete) and https://core.telegram.org/method/messages.forwardMessages (noforwards). Implementation is new Go code, not copied Python. Telegram deletion limits: https://core.telegram.org/bots/api#deletemessage .

Bot lookup rate limit: 12/minute per Telegram chat, burst 4, bounded 4096-entry map and 8 concurrent catalog lookups. Mongo connection pool capped at 10. Runtime RAM with MongoDB driver has not been measured.

## Website integration and link refresh

An expiring URL is not a permanent catalog URL. Store the Telegram message ID in your website database along with title/course/lesson. Website BACKEND must verify student's login/enrollment before requesting a new link. Never send MINT_API_KEY or SIGNING_SECRET to frontend JavaScript.

Generate a DIFFERENT random secret and set MINT_API_KEY (blank disables the endpoint). From trusted backend:

```sh
# Environment variables below belong on the backend, never in browser JS.
curl -X POST "$STREAM_HOST/api/link/$MESSAGE_ID" \
  -H "Authorization: Bearer $MINT_API_KEY"
```

Response: JSON with url and expires. Set video.src to url. Request fresh URL before expiry, or retry once with a refreshed URL on a 403 and restore video.currentTime. No automated player refresh implementation is included. Existing connections may finish after link expiry; new range requests cannot. A valid URL is shareable until expiry; this is not DRM or per-student binding. Rotating SIGNING_SECRET invalidates all old URLs.

### Video ID API: GET /api/stream?videoId=SLUG

Lets your website ask for a fresh 4 hour stream URL using the lecture slug (the ID you set in the bot after upload). Needs MINT_API_KEY set (add it in Railway Variables, 32+ random characters, different from SIGNING_SECRET). Without it the endpoint is not registered. Slugs come from MongoDB, so MONGODB_URI must be working.

```sh
curl "$STREAM_HOST/api/stream?videoId=6a2698f9735cb5428a449a0a" \
  -H "X-API-Key: $MINT_API_KEY"
```

Success (200):

```json
{"videoId":"6a2698f9735cb5428a449a0a","url":"https://your-host/stream/123?hash=...&expires=...&sig=...","expires_at":"2026-10-10T03:30:00Z","expires":1791603000}
```

Errors are JSON like {"error":"not_found","message":"..."}: 401 unauthorized (missing or wrong key), 400 bad_video_id, 404 not_found, 429 rate_limited (per IP, see rate limit settings), 503 catalog_unavailable or catalog_error. The key can also be sent as Authorization: Bearer KEY.

The key must stay on YOUR website backend. A browser page cannot call this directly without exposing the key, and there is no key-free mode on purpose (anyone with a slug could then mint links forever). Flow: player page calls your backend, e.g. /my-video?videoId=abc, your backend checks the student is logged in, calls this API with the key, and returns {url} to the page. Node example for the backend:

```js
// Express route on YOUR website backend
app.get('/my-video', async (req, res) => {
  // check the user's login/enrollment here first
  const r = await fetch(`${process.env.STREAM_HOST}/api/stream?videoId=${encodeURIComponent(req.query.videoId)}`,
    { headers: { 'X-API-Key': process.env.MINT_API_KEY } });
  res.status(r.status).json(await r.json());
});
```

Player page:

```js
const { url, expires_at } = await (await fetch('/my-video?videoId=' + id)).json();
video.src = url; // refetch before expires_at, or on a 403, and restore currentTime
```

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
go test -race ./internal/security ./internal/catalog
go test -p 1 ./...
go vet -p 1 ./...
go build -p 1 -o spark-stream ./cmd/fsb
```

Security tests cover HMAC tampering/expiry, ranges, safe headers and rate limiter bounds/concurrency. CI runs full compile/test/vet. No real Telegram playback, Docker deployment, 512MB load test or third-party dependency security audit was performed during initial preparation. CDN redirects are still unsupported upstream; some Telegram files may fail. Upstream file-reference refresh after expiry is not implemented; stale metadata can need retry/restart. See docs/TESTING.md for current local verification status.

## License

AGPL-3.0 inherited from upstream. Preserve attribution. A private repo does not remove network-use source obligations: offer corresponding source to users who interact with your modified service as required by the license. This is not legal advice. Keep secrets out of source offers.
