# AlertMe Flutter app — build spec

## Backend contract (LIVE, us-east-2)
- API base: `https://x7qtg546oa.execute-api.us-east-2.amazonaws.com`
- Cognito region `us-east-2`, UserPoolId `us-east-2_2L2LUi3h6`, App client id `3qfmo7gu8ouim595dfp32qljju` (no client secret; auth flow USER_SRP_AUTH; email sign-in).
- Auth: every request except `/health` sends `Authorization: Bearer <Cognito ID token>` (ID token, not access token — the API reads the `email` claim). Refresh with the refresh token when expired.

## Routes (JSON)
- `POST /devices` `{token, platform:"ios"|"android"}` → 204. Call after sign-in with the FCM token; re-call on token refresh.
- `POST /invites` `{}` → 201 `{code, expires_at}` (share code out-of-band).
- `POST /invites/{code}/accept` → 200 `{pairing}`; 404 if bad/expired; 400 self-pair.
- `GET /pairings` → 200 `{pairings:[Pairing]}`.
- `POST /pairings/{peerID}/block` → 204.
- `POST /pages` `{recipient_id, message, idempotency_key}` → 201 `{page}` (200 on idempotent replay). 403 if no active pairing. message ≤ 500 chars. idempotency_key = a client-generated uuid v4.
- `GET /pages?since=<rfc3339>` → 200 `{pages:[Page]}` (poll for inbox+sent; omit `since` for all).
- `GET /pages/{id}` → 200 `{page, events:[PageEvent]}`.
- `POST /pages/{id}/ack` `{note?}` → 200 `{page}` (recipient only; idempotent). This STOPS the nags.
- `POST /pages/{id}/delivered` → 204 (recipient's push handler calls this on receipt).

## Model JSON shapes
- Page: `{id, sender_id, recipient_id, message, state, attempt, created_at, expires_at, acked_at?, ack_note?}`; state ∈ created|pushed|delivered|seen|acked|expired|cancelled.
- Pairing: `{user_id, peer_id, peer_name, status:"active"|"blocked", created_at}`.
- PageEvent: `{page_id, type, detail?, at}`; type ∈ created|push_attempt|delivered|seen|acked|expired|cancelled|no_devices.

## Push payload received (Android, data-only FCM v1)
- `data` keys: `page_id`, `attempt`, `sender_name`. No `notification` block — the app MUST build the notification itself (foreground AND background/killed handlers).
- On receipt: (1) `POST /pages/{id}/delivered`; (2) show a **full-screen, high-importance alarm** notification (fullScreenIntent, IMPORTANCE_MAX channel, looping/insistent sound, vibration) titled with sender_name; (3) tapping opens the Incoming Page screen which fetches `GET /pages/{id}` for the message and offers **Ack** (+ optional note e.g. "on my way").

## App scope (v1 screens)
1. Auth: sign up (email+password → email verification code), sign in, sign out. Persist session (secure storage).
2. Home: tabs/sections for Contacts (pairings) and Activity (pages history, newest first, showing state).
3. Contacts: create invite (show/share code), accept invite (enter code), block.
4. Send page: pick an active contact, type message, send (generate idempotency_key uuid).
5. Incoming page (alarm): big sender name + message, prominent ACK button, optional note. Ack → success, nags stop.
6. Sent page detail: timeline from events (delivered/seen/acked), live-ish via poll.

## Tech choices (use these)
- State: `flutter_riverpod` (or provider) — pick one, keep it simple.
- Cognito: `amazon_cognito_identity_dart_2` (pure Dart SRP; no client secret). Store tokens in `flutter_secure_storage`.
- HTTP: `dio` or `http` with an auth interceptor that attaches the ID token and refreshes on 401.
- Push: `firebase_core` + `firebase_messaging`; notifications: `flutter_local_notifications` (Android channel IMPORTANCE_MAX + fullScreenIntent). Background handler via `@pragma('vm:entry-point')` top-level function.
- IDs: `uuid` for idempotency keys.
- Config: a `lib/config.dart` with the API base + Cognito ids above (checked in — not secret).

## External config the app needs (document in SETUP.md, use placeholders)
- Firebase Android app + `android/app/google-services.json` (NOT committed; placeholder + instructions). Without it FCM won't init.
- Firebase project's service-account JSON → used to create the SNS FCM platform application (backend side, cdk.json `fcmPlatformAppArn`), so push actually leaves AWS.
- Min Android SDK 23+; `POST_NOTIFICATIONS` runtime permission (Android 13+), `USE_FULL_SCREEN_INTENT` permission, high-priority FCM.

## Definition of done for the build agent
- App under `/Users/dgar/Documents/Alertme/app`.
- `~/flutter/bin/flutter analyze` is CLEAN (no errors; warnings acceptable if unavoidable, but aim for zero).
- `SETUP.md` documents the Firebase/Cognito/Android-SDK steps to actually run it.
- Do NOT invent backend routes/fields — match this contract exactly.
