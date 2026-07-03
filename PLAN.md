# AlertMe — Product & Engineering Plan

A simple, polished app that lets people **page** each other: an urgent, can't-miss
alert that cuts through silent mode and Do Not Disturb, and keeps nagging until the
recipient acknowledges it. Think "a pager for people who matter to each other" —
family, close friends, small teams — with modern delivery guarantees under the hood.

---

## 1. Product definition

### Core loop

1. **Alice pages Bob** — picks a contact, optionally adds a short message and an
   urgency level, hits Page.
2. **Bob's phone goes off** — a loud, distinctive alert that bypasses silent/DND
   (with his prior consent), repeating until he responds.
3. **Bob acknowledges** — one tap: "Got it", "On my way", "Call you in 5", or a
   custom reply.
4. **Alice sees the status live** — sent → delivered → seen → acknowledged, with
   timestamps.

The acknowledgment loop is the product. Messaging apps tell you a message was
delivered; a pager tells you the *human* received it.

### Feature set

**MVP (v1)**

- Phone-number signup with SMS OTP (contact discovery works out of the box).
- Mutual-consent pairing: you can only page someone who accepted your page request.
  This is the abuse-prevention cornerstone — no unsolicited pages, ever.
- One-tap page with 3 urgency levels:
  - **Ping** — normal push notification.
  - **Page** — repeating alert, bypasses silent mode (critical alert / full-screen intent).
  - **Emergency** — everything above, plus auto-retry every 60s and SMS fallback
    if unacknowledged after N minutes.
- Delivery/ack status timeline visible to the sender.
- Quick-reply acknowledgments + free-text reply.
- Per-contact permission controls: which urgency levels each contact may use,
  quiet-hours exceptions, mute/block/unpair.
- Page history.

**v1.5**

- Groups ("page my roommates", "page the on-call parent") — first-to-ack or all-must-ack.
- Escalation chains: "if Bob doesn't ack in 5 min, page Carol."
- Scheduled/recurring pages (medication reminders, pickup times).
- Apple Watch / Wear OS ack from the wrist.

**v2 / monetization**

- Small-team tier: shared rotas, simple on-call schedule, escalation policies,
  audit log — a lightweight PagerDuty for vets, locksmiths, restaurants, churches.
- Free tier: unlimited Pings, capped Pages/month; paid removes caps and unlocks
  groups + escalation.

### What we deliberately don't build

- General chat (link out to SMS/WhatsApp after the ack; we are the *interrupt*,
  not the conversation).
- Feeds, stories, social graph beyond direct pairings.
- Location sharing (maybe a v2 opt-in "share location with this page").

---

## 2. Client apps

### Platform choice

**Native iOS (Swift/SwiftUI) and native Android (Kotlin/Jetpack Compose).**

This app's entire value lives in each OS's most privileged notification paths, and
those APIs are platform-specific, fiddly, and where cross-platform frameworks are
weakest. Everything outside the alert path (screens, settings, history) is simple
CRUD UI — the part React Native/Flutter would help with is the cheap part. Going
native also matters for the "more polished" goal: haptics, sound design, live
activities, widgets.

If team size forces a single codebase, Flutter with native platform channels for
the alert path is the fallback — but plan on writing the critical-alert code
natively either way.

### iOS specifics

- **Critical Alerts entitlement** (`com.apple.developer.usernotifications.critical-alerts`)
  — bypasses silent switch and DND. Requires an Apple approval request; apply on
  day one (weeks of lead time). Design the request around safety/urgent-coordination
  use cases. Fallback until granted: Time Sensitive notifications + repeated pushes.
- Time Sensitive interruption level for Page tier if critical alerts are denied.
- **Live Activity** on the sender's side showing the page's live status on the
  lock screen / Dynamic Island.
- Notification Service Extension to fetch page details, play the right sound, and
  post delivery receipts even when the app isn't running.
- PushKit is *not* usable for this (VoIP-only enforcement); don't plan around it.
- Custom ringtone-length alert sounds (≤30s), re-fired by repeated pushes for the
  nag loop.

### Android specifics

- **Full-screen intent** (`USE_FULL_SCREEN_INTENT`) for Page/Emergency — the
  incoming-call-style takeover screen with big Acknowledge button. Play Store
  policy now gates this permission; our use case (time-critical alerts requiring
  user response) is the sanctioned one, but write the policy justification early.
- Notification channels: separate channels per urgency so the OS-level settings
  match our tiers; Page/Emergency channels set to bypass DND
  (`setBypassDnd`, needs user grant via `ACCESS_NOTIFICATION_POLICY` flow).
- FCM **high-priority** data messages + a foreground service fallback for the nag
  loop; handle Doze and OEM battery killers (onboarding flow that walks users
  through whitelisting on Samsung/Xiaomi/etc. — this is a known reliability sink,
  budget real time for it).
- Wake lock + custom alarm-style sound via `MediaPlayer` on the alarm stream for
  Emergency tier (alarm stream ignores media volume/silent mode).

### Shared client architecture

- Thin clients: all state server-authoritative; clients render and ack.
- Local persistence (SQLite/Room/GRDB) so history and pending acks survive offline.
- Ack is idempotent and queued: if Bob acks while offline, it syncs on reconnect
  and the server reconciles (SMS-fallback suppression, sender timeline).
- WebSocket (or SSE) session while foregrounded for instant status updates;
  push-driven otherwise.

---

## 3. Backend

### Shape

**A single modular monolith** (not microservices) — this product is one workflow.
Split later only if scale demands it. Two components from day one, though:

1. **API service** — auth, pairing, pages CRUD, device registry, WebSocket fan-out.
2. **Delivery worker** — consumes a queue, owns the state machine below, talks to
   APNs/FCM/SMS. Isolated so a push-provider slowdown never blocks the API.

**Stack recommendation:** TypeScript on Node (NestJS or Fastify) or Go. Both have
first-class APNs/FCM/Twilio SDKs and hire easily. Pick whichever the team knows —
delivery correctness, not language, is the hard part here.

### The page state machine (the heart of the system)

```
created → queued → pushed → delivered → seen → acknowledged
                     │          │
                     ├─ retry (backoff, per-tier schedule)
                     ├─ escalate (next device → all devices → SMS → escalation contact)
                     └─ expired / cancelled / failed
```

- Every page is a row with a state, owned by the delivery worker.
- Transitions are event-sourced into a `page_events` table — this gives the sender
  timeline, debugging, and the audit log for the team tier, all from one design.
- Retries/escalations are **durable scheduled jobs** (not in-memory timers): a
  delayed-job queue keyed by page ID, so a worker crash never loses a nag cycle.
- All client-facing mutations (send, ack) carry an idempotency key.

### API surface (v1)

```
POST /auth/otp/request        POST /auth/otp/verify
GET/POST /pairings            POST /pairings/{id}/accept|block
GET/PATCH /me/settings        POST /devices  (push-token registry)
POST /pages                   POST /pages/{id}/ack
POST /pages/{id}/cancel       GET  /pages?since=…
WS   /realtime                (status fan-out to online clients)
```

### Data model (Postgres)

- `users` — phone (E.164, unique), display name, settings JSONB.
- `devices` — user_id, platform, push_token, last_seen, capabilities
  (critical-alerts granted? full-screen granted?). Multiple devices per user.
- `pairings` — requester, recipient, status (pending/active/blocked),
  per-pairing permissions (max urgency, quiet-hours override).
- `pages` — sender, recipient, pairing_id, tier, message, state, idempotency_key,
  expires_at.
- `page_events` — page_id, event type, actor, device_id, timestamp (append-only).
- `escalation_policies` (v1.5) — steps, delays, targets.

### Delivery pipeline

- **APNs** direct via token-based auth (p8 key) for critical alerts; **FCM** HTTP v1
  for Android. Handle token feedback (unregistered → prune device row).
- **SMS fallback** via Twilio (or MessageBird) for Emergency tier and for OTP.
  SMS is the reliability backstop when the phone has no data or the app was killed.
- Per-tier delivery policy table (retry cadence, max attempts, fallback ladder)
  in config, not code, so tuning doesn't need a deploy.
- Delivery receipts: the client's notification extension calls back
  `POST /pages/{id}/events {type: delivered}`; absence of a receipt within the
  window is itself a signal that triggers the next rung of the ladder.

---

## 4. Infrastructure

### Hosting & runtime

- **One cloud, managed everything, boring choices.** AWS (or GCP — pick one):
  - API + workers in containers: ECS Fargate or Cloud Run. No Kubernetes at this size.
  - **Postgres**: RDS/Cloud SQL with a read replica and PITR backups.
  - **Redis**: ElastiCache — delayed-job queue (BullMQ if Node), WebSocket pub/sub,
    rate-limit counters, OTP throttling.
  - **Queue**: Redis-backed jobs to start; SQS if we outgrow it.
- Two environments (staging, prod) from day one; prod in 2+ AZs.
- Everything in **Terraform** from the first week — this stack is small enough
  that IaC is nearly free now and painful to retrofit.

### CI/CD

- GitHub Actions: lint + typecheck + unit tests on PR; deploy to staging on merge
  to main; tagged release → prod with one-click promote and instant rollback
  (previous task definition).
- Mobile: Fastlane pipelines → TestFlight / Play internal track on every main
  merge; phased production rollouts.
- DB migrations run as a gated deploy step (e.g. Atlas / Prisma Migrate / golang-migrate).

### Observability — this product *is* its reliability

- **Metrics (the ones that matter):** page-to-delivered p50/p95/p99,
  delivered-to-acked, fallback-ladder activation rate, push provider error rates,
  OTP delivery rate. Dashboard + alerts in Grafana Cloud / Datadog.
- **We page ourselves with our own app** when page-delivery p95 breaches SLO
  (dogfooding as monitoring), with PagerDuty/Opsgenie as the independent backstop.
- Sentry on clients and server; structured logs with page_id as the correlation
  key end-to-end (client → API → worker → provider response).
- Synthetic canary: a bot account pages another bot account on real devices
  (physical device farm or two phones on a shelf) every 5 minutes and measures
  true end-to-end latency including the OS layer.

### Security & privacy

- Message content encrypted at rest (KMS envelope); TLS 1.2+ everywhere; push
  payloads carry only the page ID + tier — content is fetched by the notification
  extension over authenticated API, so plaintext never transits APNs/FCM.
- Short-lived JWT access tokens + rotating refresh tokens bound to the device row;
  server-side revocation on unpair/logout/device removal.
- Rate limits at every abuse surface: OTP requests (per-phone and per-IP), page
  sends per pairing per hour, pairing requests per day.
- Data minimization: contacts matched via hashed phone numbers, never stored raw;
  page content auto-purged after a retention window (e.g. 90 days) unless the
  user opts to keep history.
- Account deletion + data export endpoints from v1 (GDPR/CCPA baseline).

### Cost posture (early stage)

Fargate + RDS small instances + ElastiCache + Twilio ≈ low hundreds of $/month
until real traction; push notifications are free. The only per-unit cost that
scales with usage is SMS — which is why SMS sits at the *bottom* of the fallback
ladder and Emergency tier is the natural paid feature.

---

## 5. Risks & mitigations

| Risk | Mitigation |
|---|---|
| Apple denies Critical Alerts entitlement | Apply immediately with a safety-coordination framing; ship v1 on Time Sensitive + repeated pushes; the nag loop still works, just doesn't pierce the mute switch |
| Google Play rejects full-screen intent use | Our use case matches the stated policy exception (time-critical, user-response-required); prepare the justification with screenshots before submission |
| OEM battery killers eat Android alerts | High-priority FCM + onboarding whitelist flow + SMS fallback rung; measure per-OEM delivery rates from day one |
| Harassment via paging | Mutual consent pairing, per-contact urgency caps, one-tap block that silently drops future pages, send-rate limits |
| SMS cost abuse | Emergency tier gated per pairing + rate-limited + eventually paid |
| Delivery pipeline bugs destroy trust instantly | Event-sourced state machine, idempotent everything, synthetic canary on real hardware, dogfood as primary alerting |

---

## 6. Milestones

| Phase | Duration | Deliverable |
|---|---|---|
| 0 — Foundations | 2 wks | Terraform envs, CI/CD, auth + OTP, device registry, Apple entitlement application submitted |
| 1 — Core loop | 4 wks | Pairing, page state machine, APNs/FCM delivery, iOS + Android apps sending/receiving/acking Ping tier |
| 2 — The pager | 4 wks | Critical alerts / full-screen intents, nag loop, retry ladder, SMS fallback, sender live timeline |
| 3 — Polish & beta | 3 wks | Sound/haptics design, onboarding (incl. Android whitelisting flow), settings, history, TestFlight/Play beta with ~50 households |
| 4 — Launch | 2 wks | Store review cycles, phased rollout, dashboards + canary live, on-call rota (paged via AlertMe) |

Roughly **15 weeks to public v1** with 2 mobile engineers (one per platform) and
1–2 backend engineers. v1.5 (groups, escalation, watch apps) follows as fast-follow.
