# AlertMe

A pager for people: send someone a page that keeps alerting until they
acknowledge it. Plans: [`PLAN.md`](PLAN.md) (full product) →
[`PLAN-LEAN.md`](PLAN-LEAN.md) (lean stack) → [`PLAN-AWS.md`](PLAN-AWS.md)
(**current architecture**: Lambda + DynamoDB + SQS + Cognito, ~$9/mo).

## Layout

```
infra/     CDK stack (TypeScript): DynamoDB, SQS, Cognito, API GW, lambdas, alarms,
           FCM SNS platform app (custom resource, infra/handlers/)
infra/gcp/ Terraform: Firebase + FCM service account on Google Cloud
backend/   Go: cmd/{api,pager,canary} lambdas + internal packages
```

## Develop

```sh
cd backend
make vet test    # unit tests
make build       # linux/arm64 bootstrap binaries → dist/<lambda>/

cd ../infra
npm install
npm run build    # typecheck
npx cdk synth    # requires backend/dist to exist (make build first)
```

## Deploy

1. `aws configure` / SSO into the target account; `npx cdk bootstrap` once.
2. Push credentials (see PLAN-AWS.md §8):
   - **FCM (Android)** is owned by the stack: the `FcmPlatformApp` custom
     resource creates the SNS platform application `fcmPlatformAppName`
     from the Secrets Manager secret `fcmCredentialSecretName`
     (`alertme/fcm-service-account`). The secret's *value* (the Google
     service-account JSON) is set out of band — it never appears in the
     repo, the template, or CFN parameters. First-time setup: apply
     `infra/gcp/` (below), then create a key and the secret as in
     "Rotating the FCM credential", using `aws secretsmanager create-secret
     --name alertme/fcm-service-account --secret-string file://sa.json`
     instead of `put-secret-value`. Empty secret name = Android push off.
   - **APNs (iOS)** is still created by hand (Apple `.p8` token auth); put
     its ARN in `apnsPlatformAppArn` in `infra/cdk.json`. Empty = iOS push
     off; the stack still deploys and push no-ops.
3. Set `alertEmail` in `infra/cdk.json` (alarm notifications).
4. `cd backend && make build && cd ../infra && npx cdk deploy`.

### Google Cloud / Firebase (`infra/gcp/`, Terraform)

The GCP project `claude-code-484803` is **pre-existing and shared** with
unrelated work: Terraform does not manage the project itself, other APIs, or
other IAM. It manages only AlertMe's pieces — the Firebase enablement, the
Android app `com.alertme.alertme`, the `firebase`/`fcm`/`iam` APIs (never
disabled on destroy), the `sns-fcm-push` service account, and that SA's
`roles/firebasecloudmessaging.admin` grant (additive `iam_member` only). All
were created by hand first and adopted with the `import {}` blocks in
`infra/gcp/imports.tf`. Service-account **keys are not in Terraform** (they
would sit in state in plaintext); they're created out of band (below).

Bootstrap / apply (Terraform >= 1.5; state is local and gitignored, and
holds no secrets):

```sh
cd infra/gcp
terraform init
# any Google credential with rights on the project, e.g. ADC:
gcloud auth application-default login     # or: export GOOGLE_OAUTH_ACCESS_TOKEN=$(gcloud auth print-access-token)
terraform plan     # first run: imports only; afterwards: "No changes"
terraform apply
```

On a fresh machine without the old state, the import blocks re-adopt the
live resources — no recreation.

### Rotating the FCM credential

1. Create a new key for the SA (out of band; never commit it):
   `gcloud iam service-accounts keys create sa.json --iam-account sns-fcm-push@claude-code-484803.iam.gserviceaccount.com`
2. Put it in the secret and delete the file:
   `aws secretsmanager put-secret-value --secret-id alertme/fcm-service-account --secret-string file://sa.json && rm sa.json`
3. Bump `fcmCredentialRotation` in `infra/cdk.json` and deploy
   (`cd backend && make build && cd ../infra && npx cdk deploy`). The custom
   resource re-reads the secret and calls `SetPlatformApplicationAttributes`
   on the same app — the ARN and registered device endpoints are kept.
4. Once push is confirmed working, delete the old key:
   `gcloud iam service-accounts keys list --iam-account …` then
   `gcloud iam service-accounts keys delete <old-key-id> --iam-account …`.

Changing `fcmPlatformAppName` instead *replaces* the platform app (new
ARN; CloudFormation deletes the old one), so devices must re-register.

Multi-region warm standby: set `replicaRegions` in `cdk.json` (global table)
and deploy the same stack to the second region. Canary: flip `canaryEnabled`
once bot credentials exist (see `cmd/canary`).

## Status

- [x] CDK stack (synths; deployable) — now includes GSI2 (sender-side page
      index) and the canary's Cognito client id
- [x] Domain model, single-table key schema, nag schedule (+tests)
- [x] API routes wired with request/response contracts
- [x] DynamoDB store implementation (`internal/store/dynamo`, +key-schema tests)
- [x] SNS mobile-push sender (`internal/push/sns`) — endpoints created at
      device registration, published to by the pager
- [x] Pager worker logic (`cmd/pager`, +tests against a fake store/sender)
- [x] Handler implementations (`internal/api/handlers.go`, +tests against a
      fake store)
- [x] Canary (`cmd/canary`) — env-driven, no-ops until bot credentials are
      provisioned
- [ ] Flutter app

## Mail watch (email → page)

`alertme-mailwatch` runs every 10 minutes (EventBridge), searches Gmail for
**unread** mail from the senders in `cdk.json` → `mailWatch.senders` (last 2
days), and pages `mailWatch.recipientUserId` once per message, from an
"AlertMe Mail" bot that pairs itself with the recipient on first run. The
Gmail message id is the page's idempotency key, so a message that stays
unread never pages twice. Access is OAuth **gmail.readonly** — the job can't
send, delete, or mark mail read. Blocking "AlertMe Mail" in the app stops it.

One-time setup (Google Cloud project `claude-code-484803`; the Gmail API is
enabled by `infra/gcp`):

1. **Google Auth Platform → Branding/Get started**: app name e.g. "AlertMe
   Mail Watch", your email as support + contact, Audience **External**.
2. **Audience → Publish app** ("In production"). In "Testing" mode Google
   expires refresh tokens after 7 days. As an unverified app you'll see a
   "Google hasn't verified this app" screen on consent — choose Advanced →
   continue (it's your own app).
3. **Clients → Create client → Desktop app**, then download the JSON.
4. `python3 tools/gmail-oauth-setup.py ~/Downloads/client_secret_*.json`
   (needs AWS CLI auth). It stores the credential in Secrets Manager
   `alertme/gmail-oauth`; nothing is printed or kept on disk.

Re-run step 4 to rotate. If the token stops working (revoked, password
change), the `MailwatchErrors` alarm emails `alertEmail` after two failed
runs. Until the secret exists, runs are logged no-ops.
