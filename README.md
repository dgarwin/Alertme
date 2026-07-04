# AlertMe

A pager for people: send someone a page that keeps alerting until they
acknowledge it. Plans: [`PLAN.md`](PLAN.md) (full product) →
[`PLAN-LEAN.md`](PLAN-LEAN.md) (lean stack) → [`PLAN-AWS.md`](PLAN-AWS.md)
(**current architecture**: Lambda + DynamoDB + SQS + Cognito, ~$9/mo).

## Layout

```
infra/     CDK stack (TypeScript): DynamoDB, SQS, Cognito, API GW, lambdas, alarms
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
2. Create the FCM secret: a Firebase service account JSON stored at SSM
   SecureString parameter `/alertme/fcm-service-account`.
3. Set `alertEmail` in `infra/cdk.json` (alarm notifications).
4. `cd backend && make build && cd ../infra && npx cdk deploy`.

Multi-region warm standby: set `replicaRegions` in `cdk.json` (global table)
and deploy the same stack to the second region. Canary: flip `canaryEnabled`
once bot credentials exist (see `cmd/canary`).

## Status

- [x] CDK stack (synths; deployable)
- [x] Domain model, single-table key schema, nag schedule (+tests)
- [x] API routes wired with request/response contracts
- [ ] DynamoDB store implementation (`internal/store/dynamo`)
- [ ] FCM sender (`internal/push/fcm`)
- [ ] Pager worker logic (`cmd/pager`)
- [ ] Handler implementations (`internal/api/handlers.go`)
- [ ] Canary (`cmd/canary`)
- [ ] Flutter app
