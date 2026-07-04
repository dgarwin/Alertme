# AlertMe — AWS Plan

Supersedes `PLAN-LEAN.md`'s hosting choices. Same product, same app, same page
state machine — moved onto managed AWS with **multi-AZ compute and database**,
a **multi-region recovery story**, and a hard budget of **<$50/month**.

Design rule: lean on AWS managed services wherever they delete code we'd
otherwise write (auth, scheduling, scaling, failover), and spend the budget on
the one thing that has a real price tag: the Multi-AZ database.

---

## 1. The budget math that picks the architecture

Multi-AZ RDS Postgres (db.t4g.micro + 20 GB) costs ~$28/month. That leaves
~$20 for everything else, which immediately decides compute:

| Compute option | Multi-AZ? | ~$/mo | Verdict |
|---|---|---|---|
| Fargate (2 ARM tasks) + ALB | yes | $14 + $17 ALB = $31 | ALB alone breaks the budget |
| Fargate (2 tasks, public IPs, API GW + Cloud Map) | yes | $14 + $7 IPv4 = $21 | ≈$54 total — just over, more moving parts |
| **Lambda + API Gateway HTTP API** | **yes, inherently, active-active** | **~$1** | **winner — multi-AZ is free** |

Lambda runs across all AZs in the region by default. The property we'd pay
Fargate ~$21/month to have — two instances in two AZs behind a load balancer —
is simply how Lambda works, at this traffic level for about a dollar. It's as
tried-and-true as AWS gets, and it also deletes the Dockerfile, the ECS config,
and OS patching.

---

## 2. Architecture

```
Flutter app (iOS + Android)
   │ HTTPS                                  ┌────────────────────────────┐
   ▼                                        │  AWS region (multi-AZ)     │
API Gateway HTTP API ── JWT authorizer ◄────┤  Cognito user pool         │
   │                                        │  (Sign in w/ Apple+Google) │
   ▼                                        └────────────────────────────┘
api Lambda (Go, whole REST API, in VPC)
   │            │
   │            └── SQS delay queue ──► pager Lambda (Go, in VPC)
   ▼                    ▲    │                 │           │
RDS Postgres            └────┘                 ▼           ▼
Multi-AZ (t4g.micro)   re-enqueue         RDS (state)   FCM → APNs/FCM
                       next nag                          (both platforms)
```

### Service by service — and the code each one deletes

- **Cognito user pool** with Apple + Google as federated identity providers.
  API Gateway's built-in **JWT authorizer** validates Cognito tokens before our
  code ever runs. *Deletes:* the entire auth module — token issuance, refresh,
  verification, `/auth/*` endpoints. Free tier covers us to 10k+ MAU.
- **API Gateway HTTP API**: routing, TLS, throttling (built-in rate limiting
  replaces hand-rolled limiter code). ~$1/million requests.
- **api Lambda**: one Go binary serving the whole REST API (chi router via the
  Lambda Go proxy — it's a normal HTTP server, testable locally, portable to
  Fargate later unchanged). 8 routes now (Cognito ate the auth ones).
- **SQS as the nag scheduler.** The lean plan's jobs table becomes a delay
  queue: creating a page sends `{page_id, attempt: 0}`; the **pager Lambda**
  loads the page, and if it's still unacknowledged, sends the push and
  re-enqueues the next attempt with `DelaySeconds` per the nag schedule
  (30s, 30s, 60s, 2m, 5m… to 30-min expiry; all ≤ SQS's 900s max delay).
  Acks don't cancel messages — the next fire sees `acked` in Postgres and drops.
  At-least-once delivery + attempt number recorded in `page_events` = no
  double alerts. A **DLQ + CloudWatch alarm** catches poison messages.
  *Deletes:* the scheduler goroutine, and makes retry durability AWS's problem.
- **RDS Postgres, Multi-AZ deployment** (db.t4g.micro, 20 GB gp3): synchronous
  standby in a second AZ, automatic failover in ~60s, automated backups with
  PITR. Same 6-table schema as the lean plan. Connection math: a handful of
  concurrent Lambdas with one connection each is nothing against t4g.micro's
  ~80-connection ceiling — **skip RDS Proxy** ($22/mo) until traffic argues.
- **Push:** FCM HTTP v1 remains the single push API for both platforms (free;
  the `apns` block carries iOS interruption level and sound). SNS mobile push
  exists but adds an endpoint-management layer for zero benefit here.
- **SSM Parameter Store** (free) for the FCM key and DB credentials — not
  Secrets Manager ($0.40/secret). **ECR** for images or just zip deploys.
- **IaC: one CDK (TypeScript) stack**, ~300 lines. Deploy via GitHub Actions
  with OIDC (no long-lived AWS keys). `cdk deploy` is the entire release
  process; Lambda versioning gives instant rollback.

### The one genuinely annoying decision: VPC egress

Lambdas must be in the VPC to reach RDS, but the pager Lambda must also reach
FCM on the internet — and a managed NAT Gateway is **$33/month + data**, which
torpedoes the budget on its own. Options, honestly:

| Option | ~$/mo | Tradeoff |
|---|---|---|
| Managed NAT Gateway | $33+ | Blows the budget; the "right" answer at funded scale |
| **2× NAT instances (t4g.nano, one per AZ)** | **$13.50** | **Default.** fck-nat AMI or Amazon Linux NAT; the only self-managed piece in the stack |
| Public RDS (TLS forced + IAM auth), Lambdas out of VPC | $0 | Cleanest ops, ~$35 total; a public 5432 is the same posture Neon/Supabase sell, but it's the first thing a security review flags |

Default to the NAT instances; swap to managed NAT Gateway the day the budget
relaxes — it's a one-line CDK change.

---

## 3. Multi-AZ and multi-region, precisely

**Multi-AZ (in budget, day one):**

- Compute: Lambda — active-active across AZs automatically. An AZ failure is
  invisible.
- Database: RDS Multi-AZ — automatic failover to the synchronous standby,
  ~60s of write unavailability; in-flight nags retry through it via SQS.
- SQS, API Gateway, Cognito, Route 53: regionally replicated managed services;
  multi-AZ is inherent.

**Multi-region (tiered, because active-active doesn't fit $50):**

- **In budget — pilot light (RTO ~30–60 min, RPO ≤ 15 min):** RDS cross-region
  automated backup replication (~$2/mo snapshot storage) + the CDK stack is
  region-agnostic by construction (`cdk deploy --context region=us-west-2`) +
  Route 53 health check on `/health`. Regional outage = restore snapshot in
  region B, deploy stack, flip DNS. A quarterly GitHub Action drill actually
  runs the restore so the runbook stays true.
- **+$15/mo — warm standby (RTO ~5 min):** cross-region read replica
  (t4g.micro, single-AZ) promoted on failover, stack pre-deployed. Total ≈$62 —
  first thing to add when the budget moves.
- **Not on the menu:** Aurora Global / active-active writes. Real regional
  failovers at this stage are rarer than the bugs that redundancy complexity
  would cause.

---

## 4. Observability (all inside free tiers)

- **CloudWatch alarms → SNS email:** pager-Lambda errors, DLQ depth > 0,
  API 5xx rate, RDS failover/CPU/storage events. ~$2–3/mo in alarms + logs
  (short retention).
- **Canary:** EventBridge rate(5 min) → a canary Lambda where bot A pages bot B
  through the real API and asserts the state machine advances; it pings a
  healthchecks.io URL only on success — a stalled *scheduler* (not just a dead
  API) produces an email within minutes. (CloudWatch Synthetics does this for
  ~$10/mo; the Lambda does it for ~$0.)
- Sentry free tier on the Flutter app; structured JSON logs with `page_id`
  correlation end-to-end.

---

## 5. Cost sheet (prod, us-east-1)

| Item | $/mo |
|---|---|
| RDS Postgres db.t4g.micro **Multi-AZ** + 2×20 GB gp3 | ~28.00 |
| 2× NAT instances (t4g.nano + public IPv4) | ~13.50 |
| Lambda + API Gateway + SQS | ~1.00 |
| Cognito, SSM, ECR, S3 | ~0.00 |
| Route 53 hosted zone + health check | ~1.00 |
| CloudWatch logs + alarms | ~3.00 |
| Cross-region backup copies (pilot light) | ~2.00 |
| **Total** | **~$48.50** |

Variants: public-RDS networking → **~$35**; add warm-standby region → ~$62.
Staging: run Postgres in Docker locally + a `staging` CDK context with
single-AZ RDS (~+$14) when the budget allows; not required to start.

---

## 6. What changed vs. the lean plan — and what didn't

| | Lean (`PLAN-LEAN.md`) | AWS |
|---|---|---|
| Compute | 1 Fly machine | Lambda, multi-AZ active-active |
| DB | SQLite + Litestream | RDS Postgres Multi-AZ, PITR |
| Nag scheduler | jobs table + goroutine | SQS delay queue + pager Lambda |
| Auth code | Apple/Google verify in Go | deleted — Cognito + JWT authorizer |
| Deploy downtime | ~10–30s | zero (Lambda versioning) |
| Regional DR | none | pilot light, drilled quarterly |
| $/mo | ~15 | ~48 |

Unchanged: the Flutter app, FCM-only push, invite-link pairing, the 6-table
schema, idempotent acks, delivery receipts, event-sourced `page_events`, and
the product rule that a page nags until a human acknowledges it. The Go code
shrinks (auth and scheduler deleted) to ~1,200–1,800 lines across two Lambdas
plus ~300 lines of CDK. Build order is the same: **prove the SQS nag loop with
`curl` and two phones before writing any UI.**
