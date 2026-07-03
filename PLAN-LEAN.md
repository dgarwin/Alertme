# AlertMe — Lean Plan

The minimum infrastructure and code to ship a paging app that actually works and
is actually reliable. Companion to `PLAN.md`; same product, ruthless scope.
Target: **1–2 people, ~$15/month, one deployable thing.**

The trick is that "lean" isn't just smaller boxes on the same diagram — a few
product decisions eliminate entire subsystems:

| Product decision | Subsystem it deletes |
|---|---|
| Pair via **invite links** (share over any existing channel) instead of contact discovery | Phone auth, SMS OTP, Twilio, contact hashing, number storage |
| Sign in with **Apple/Google** only | OTP flow, password reset, most of auth code |
| Ship on **Time Sensitive (iOS) + full-screen intent (Android)**; treat Critical Alerts entitlement as a later upgrade | Weeks of entitlement lead time blocking launch |
| **No SMS fallback** in v1 — the nag loop (repeat pushes) is the fallback | Twilio, per-message costs, the whole cost-abuse problem |
| Sender polls for status while screen is open | WebSockets, pub/sub, Redis |
| One tier at launch: **Page** (nagging, ack-required). Add Ping/Emergency later | Per-tier policy machinery |

What survives untouched from the full plan, because it *is* the product:
mutual-consent pairing, the durable page state machine, idempotent acks,
delivery receipts, and the nag-until-acknowledged loop.

---

## 1. The whole system

```
┌─────────────────────┐         ┌──────────────────────────────┐
│  One mobile app     │  HTTPS  │  One server binary (Go)      │
│  (Flutter, iOS +    │◄───────►│  API + nag loop in-process   │
│   Android)          │         │  SQLite + Litestream → R2    │
└─────────────────────┘         └──────────────┬───────────────┘
        ▲                                      │ FCM HTTP v1
        └───── push (APNs via FCM, FCM) ───────┘
```

Four pieces total:

1. **One Flutter app** for both platforms.
2. **One Go binary** on one Fly.io machine (Railway/Render equally fine).
3. **SQLite** in-process, continuously replicated to object storage by **Litestream**.
4. **FCM** as the single push API — it delivers to Android natively and to iOS by
   relaying through APNs (the `apns` override block carries interruption level and
   sound). One credential, one SDK, both platforms, free.

That's the entire infrastructure. No Postgres server, no Redis, no queue, no
load balancer to manage, no Kubernetes, no second service.

---

## 2. The server (~1,500–2,500 lines of Go)

### Why one binary + SQLite is not a toy

- Traffic shape: a page is one row insert + a handful of pushes. Even 10k daily
  active users is trivially inside one small VM and SQLite's write capacity
  (WAL mode; this workload is thousands of times below its ceiling).
- **Durability**: Litestream streams every WAL frame to Cloudflare R2/S3.
  Worst-case data loss on machine death is seconds, and restore is one command.
- **The scheduler is a table, not a service.** No Redis queue — a `jobs` table:

```sql
CREATE TABLE jobs (
  id INTEGER PRIMARY KEY,
  page_id TEXT NOT NULL,
  run_at INTEGER NOT NULL,     -- unix seconds
  kind TEXT NOT NULL           -- 'push_attempt' | 'expire'
);
```

  A goroutine ticks once per second: `SELECT … WHERE run_at <= now`, sends the
  push, records the attempt, inserts the next job per the nag schedule
  (e.g. 0s, 30s, 60s, 2m, 5m… until ack or 30-min expiry). Because jobs live in
  the same SQLite transaction as the page state, a crash or deploy never loses a
  nag cycle — the restarted binary just picks up where the table says.

### Endpoints (the complete v1 API — 9 routes)

```
POST /auth/apple | /auth/google      → verify token, upsert user, issue JWT
POST /devices                        → register FCM token
POST /invites                        → create invite link (code)
POST /invites/{code}/accept          → creates active pairing
POST /pairings/{id}/block            → silently drop future pages
POST /pages          (idempotency key) → create page, enqueue first push
POST /pages/{id}/ack (idempotent)      → ack, cancel remaining jobs, push status to sender
POST /pages/{id}/delivered             → delivery receipt from notification handler
GET  /pages?since=…                    → history + poll for status
```

### Tables (the complete schema — 6 tables)

`users`, `devices`, `invites`, `pairings`, `pages`, `page_events` (+ `jobs`).
Same shapes as the full plan, minus phone numbers and escalation policies.

Push payloads carry only `{page_id}`; the app fetches content over the API.
Nothing sensitive ever transits FCM, which also keeps the crypto story trivial:
TLS + encrypted disk + private R2 bucket, done.

---

## 3. The app (one Flutter codebase, ~4–6k lines)

- **Screens (5):** sign-in, contact list, send page, incoming full-screen ack,
  page detail/history. Settings is a sheet, not a screen.
- **Android:** `flutter_local_notifications` + a ~200-line Kotlin platform channel
  for the full-screen intent, DND-bypass channel, and alarm-stream sound. This
  small native shim is unavoidable and is where the pager magic lives.
- **iOS:** Time Sensitive notifications + a Notification Service Extension
  (~150 lines Swift) that fetches page content, plays the alert sound, and posts
  the delivery receipt. The server-side nag loop re-fires the alert every cycle,
  which is what makes silent-mode-piercing *less* critical on day one.
- **Ack offline-safe:** acks queue locally and retry; server acks are idempotent.
- **Status updates:** sender screen polls `GET /pages?since` every 3s while open;
  a silent push nudges the app when state changes. No socket code at all.

---

## 4. Deploy & operations

| Concern | Solution | Cost |
|---|---|---|
| Hosting | 1× Fly.io shared-cpu machine (or Railway/Render) | ~$5–10/mo |
| Database | SQLite on a Fly volume | included |
| Backup/DR | Litestream → Cloudflare R2 | ~$0 |
| Push | FCM (both platforms) | $0 |
| TLS + domain | Fly-managed cert + one domain | ~$12/yr |
| Deploy | GitHub Actions → `fly deploy` (build, test, release) | $0 |
| Crash reporting | Sentry free tier (app + server) | $0 |
| Uptime + heartbeat | healthchecks.io pings `/health`; the nag goroutine pings a heartbeat URL each minute — if the *scheduler* stalls, you get an email even though the API looks healthy | $0 |
| Canary | Nightly GitHub Action: bot A pages bot B via the API and asserts the state machine reaches `pushed` with sane latency | $0 |

**Total: roughly $15/month**, one `Dockerfile`, one `fly.toml`, one workflow file.

### Reliability, honestly stated

What this design genuinely gives you:

- No lost pages: state + jobs are transactional in one durable, replicated DB.
- No lost acks: idempotent + client-queued.
- Crash/deploy safe: the nag loop resumes from the jobs table on boot.
- Data safe: continuous offsite replication, one-command restore.
- You find out when it breaks: dead-man heartbeat + uptime checks + Sentry + canary.

What you're consciously accepting (and when to upgrade):

- **~10–30s of downtime on deploy** and single-region. Fine for launch; pages
  created during a blip fail visibly on the sender's screen (never silently).
  *Upgrade path:* second Fly machine + LiteFS, or swap SQLite for managed Postgres —
  the code is a repository interface away.
- **No pierce-the-mute-switch on iOS** until the Critical Alerts entitlement is
  granted — apply at launch, ship the upgrade in a point release.
- **No SMS backstop**: a phone with no data gets the page late (FCM delivers on
  reconnect). Add Twilio behind the Emergency tier when someone will pay for it.
- **No cross-device sync niceties** beyond re-fetch on open.

---

## 5. Order of work (~6–8 weeks, 1–2 people)

1. **Week 1** — Go skeleton: auth, devices, pairings, SQLite migrations,
   Fly deploy + Litestream + healthcheck live from day 2.
2. **Weeks 2–3** — Page state machine + jobs loop + FCM send; prove the full nag
   cycle with `curl` and two test phones before writing any UI.
3. **Weeks 3–5** — Flutter app: sign-in, invite flow, send, ack screen; the
   Kotlin full-screen shim and Swift notification extension.
4. **Week 6** — Polish the two moments that matter (the alert going off; the
   sender watching the timeline), Sentry, canary, store submissions.
5. **Weeks 7–8** — Review-cycle buffer + beta with ~10 households.

The first thing to build is the thing everything else depends on and the thing
this document exists to protect: **the durable nag loop.** Everything else is
forms.
