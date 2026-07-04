# AlertMe — AWS Plan (DynamoDB revision)

Supersedes `PLAN-LEAN.md`'s hosting choices. Same product, same app, same page
state machine — on managed AWS with **multi-AZ compute and database**, a
**multi-region data layer**, and a hard budget of **<$50/month** (this
revision lands at ~$10).

Design rule: lean on AWS managed services wherever they delete code or ops
we'd otherwise own (auth, scheduling, scaling, failover, replication).

> **Revision note:** v1 of this plan used Multi-AZ RDS Postgres (~$48.50/mo
> total; see git history). Switching to DynamoDB removed the two biggest costs
> — the database instance ($28) and, because DynamoDB needs no VPC, the NAT
> instances ($13.50) — and upgraded multi-region from pilot-light to an
> active-active data layer. The price is query flexibility, covered in §3.

---

## 1. Why DynamoDB changes the shape, not just the bill

- **Multi-AZ by construction.** Every DynamoDB write is synchronously
  replicated across three AZs before it's acknowledged. The property we were
  buying with RDS Multi-AZ (~$28/mo, 60s failover) is DynamoDB's baseline, with
  no failover event at all, for on-demand pennies.
- **The VPC disappears.** RDS forced Lambdas into a VPC, which forced NAT for
  FCM egress — the ugliest corner of v1. DynamoDB is a regional API endpoint:
  Lambdas run outside any VPC with normal internet access. No subnets, no NAT,
  no security groups, ~40 fewer lines of CDK, and one less thing to break.
- **Multi-region becomes real.** A DynamoDB **global table** replicates to a
  second region in ~1s for roughly 2× write cost — at our volume, ~$1/mo. With
  the (stateless, pay-per-use) API stack deployed in both regions behind
  Route 53 failover, a regional outage means DNS flips and the app keeps
  working against live data. RTO minutes, RPO ~1s — v1's warm standby cost
  $62/mo and did less.
- **Ops we stop owning:** storage scaling, connection limits, engine upgrades,
  maintenance windows, PITR is a checkbox ($0.20/GB-mo ≈ pennies).

---

## 2. Architecture

```
Flutter app (iOS + Android)
   │ HTTPS                                  ┌────────────────────────────┐
   ▼                                        │  Cognito user pool         │
API Gateway HTTP API ── JWT authorizer ◄────┤  (Sign in w/ Apple+Google) │
   │                                        └────────────────────────────┘
   ▼
api Lambda (Go, whole REST API — no VPC)
   │            │
   │            └── SQS delay queue ──► pager Lambda (Go)
   ▼                    ▲    │                 │           │
DynamoDB                └────┘                 ▼           ▼
(global table,         re-enqueue        DynamoDB      FCM → APNs/FCM
 3-AZ, PITR, TTL)      next nag          (state)       (both platforms)
```

### Service by service

- **Cognito user pool** with Apple + Google federation; API Gateway's built-in
  **JWT authorizer** rejects unauthenticated requests before our code runs.
  *Deletes:* the entire auth module. Free at our MAU.
- **API Gateway HTTP API** — routing, TLS, throttling. ~$1/M requests.
- **api Lambda** — one Go binary, whole REST API (chi router behind the Lambda
  proxy; a normal HTTP server, testable locally, portable later). 8 routes.
- **SQS delay queue as the nag scheduler** — unchanged from v1: page creation
  enqueues `{page_id, attempt: 0}`; the **pager Lambda** re-reads page state,
  drops if acknowledged, otherwise pushes via FCM and re-enqueues the next
  attempt with `DelaySeconds` (30s, 30s, 60s, 2m, 5m… to 30-min expiry, all
  ≤ 900s). DLQ + alarm for poison messages. Duplicate fires are deduped by
  conditional writes on attempt number.
- **DynamoDB, on-demand, single table** (see §3), PITR on, **TTL** on invites
  and expired page items — the lean plan's "auto-purge after retention window"
  becomes a table setting instead of a cron job.
- **Push:** FCM HTTP v1 for both platforms (free), unchanged.
- **SSM Parameter Store** (free) for the FCM key; no DB credentials to store —
  DynamoDB access is the Lambda's **IAM role**, so there is no database
  password anywhere in the system.
- **IaC:** one CDK (TypeScript) stack, now ~250 lines. GitHub Actions + OIDC;
  `cdk deploy` releases, Lambda versioning rolls back.

---

## 3. Data model — the real cost of DynamoDB

The discipline: DynamoDB requires knowing your access patterns up front and
gives you no ad-hoc SQL later. We can pay that confidently because the API is
8 routes and every query is known:

**Single table, `alertme`** (PK / SK), with one GSI:

| Entity | PK | SK | Notes |
|---|---|---|---|
| User profile | `USER#<id>` | `PROFILE` | settings inline |
| Device | `USER#<id>` | `DEVICE#<token>` | capabilities, last_seen |
| Pairing | `USER#<id>` | `PAIR#<peer_id>` | written to both users in one transaction |
| Invite | `INVITE#<code>` | `META` | TTL = expiry |
| Page | `PAGE#<id>` | `META` | state, tier, message, idempotency key |
| Page event | `PAGE#<id>` | `EVT#<ts>#<type>` | append-only timeline |

**GSI1** (`GSI1PK = USER#<recipient>`, `GSI1SK = <created_at>`) on page items →
inbox, history, and `GET /pages?since=…` polling. Sender history mirrors it via
a second projection attribute.

Access patterns each map to one `GetItem`/`Query`. Correctness tools are
arguably *better* than v1's SQL:

- **Idempotent page creation:** `TransactWriteItems` — page item with
  `attribute_not_exists(PK)` on the idempotency key + first event + SQS send
  after commit.
- **Idempotent ack:** conditional update `state ∈ {pushed, delivered, seen}` →
  `acked`; a losing duplicate fails the condition and is dropped.
- **Pairing consent:** one transaction writes both directions or neither.

What we genuinely give up, and the mitigations:

- **Ad-hoc queries / analytics** (debugging "show me all pages that expired
  yesterday"): PartiQL console covers simple cases; when real analytics are
  needed, DynamoDB's built-in **export to S3 + Athena** gives full SQL over
  snapshots for pennies, no pipeline to build.
- **Schema migrations** become item-versioning discipline (a `v` attribute and
  read-time upgrades) rather than `ALTER TABLE`.
- **Relational fallback:** if the team ever hates it, the repository layer is
  the only thing that touches DynamoDB; the state machine and handlers don't
  know. But at 8 routes and 6 entities, single-table DynamoDB is squarely
  inside its sweet spot — this is the workload it was built for.

---

## 4. Multi-AZ and multi-region, precisely

- **Multi-AZ, day one, no action required:** Lambda active-active across AZs;
  DynamoDB synchronously 3-AZ; SQS/API Gateway/Cognito/Route 53 inherently
  multi-AZ. There is no failover event anywhere in an AZ outage.
- **Multi-region, in budget (~+$2/mo):** DynamoDB **global table** replica in
  region B (RPO ~1s) + the same CDK stack deployed there (Lambda/API GW/SQS
  cost ~$0 idle — pay-per-use is what makes a warm second region affordable) +
  Route 53 health-check failover on `/health`. RTO: minutes, mostly DNS TTL.
- **The honest caveat — Cognito is regional.** Tokens are JWTs validated
  against public JWKS, so **existing sessions keep working in region B**; what
  breaks during a Cognito-region outage is new sign-ins and token refresh
  (≤1 h impact for active users). Acceptable for v1; document it in the
  runbook rather than engineering around it.
- SQS is regional: in-flight nags in a dead region resume via a safety-net
  EventBridge rule in region B that re-enqueues unacknowledged pages found in
  the (replicated) table. ~30 lines; the drill tests it quarterly.

---

## 5. Observability (unchanged from v1, all ~free)

- CloudWatch alarms → SNS email: pager-Lambda errors, **DLQ depth > 0**,
  API 5xx, DynamoDB throttles (should never fire on on-demand).
- Canary: EventBridge rate(5 min) → Lambda where bot A pages bot B through the
  real API and asserts the state machine advances; pings healthchecks.io only
  on success, so a stalled scheduler emails you within minutes.
- Sentry free tier on the app; structured logs keyed by `page_id`.

---

## 6. Cost sheet (prod, us-east-1 + us-west-2 replica)

| Item | $/mo |
|---|---|
| DynamoDB on-demand + PITR + storage | ~1.00 |
| Global table replica (region B writes + storage) | ~1.00 |
| Lambda + API Gateway + SQS (both regions) | ~1.50 |
| Cognito, SSM, ECR | ~0.00 |
| Route 53 zone + 2 health checks | ~1.50 |
| CloudWatch logs + alarms (both regions) | ~4.00 |
| **Total** | **~$9** |

Headroom to the $50 cap: ~$40/mo — enough to add a single-AZ staging copy of
the whole stack (~$2, it's all pay-per-use) *and* absorb 100× traffic growth
before anything needs rethinking.

---

## 7. What changed vs. the RDS revision — and what didn't

| | v1 (RDS) | v2 (DynamoDB) |
|---|---|---|
| Database | RDS Postgres Multi-AZ, 60s failover | DynamoDB, 3-AZ synchronous, no failover event |
| Networking | VPC + 2 NAT instances | none — no VPC at all |
| DB credentials | password in SSM | none — IAM role |
| Multi-region | pilot light (RTO 30–60 min) | active-active data, warm stack (RTO minutes) |
| Retention purge | cron/job | TTL attribute |
| Query flexibility | full SQL | known patterns + Athena-on-export |
| $/mo | ~48.50 | ~9 |

Unchanged: Flutter app, FCM-only push, Cognito auth, SQS nag scheduler,
invite-link pairing, idempotent acks, delivery receipts, event-sourced
timeline. Go code ~1,200–1,800 lines across two Lambdas + ~250 lines CDK.
Build order still: **prove the SQS nag loop with `curl` and two phones before
any UI.**
