# AlertMe Mail Watch — Privacy Policy

_Last updated: 2026-11-16_

AlertMe Mail Watch is a personal automation operated by the owner of this
repository for their own Google account. It is not offered to the public.

## What it accesses

With your consent, it uses the Gmail API with the **read-only** scope
(`https://www.googleapis.com/auth/gmail.readonly`). Every 10 minutes it
searches for **unread** messages from a short, fixed list of sender addresses
and reads only each matching message's **From** and **Subject** headers.

It cannot send, delete, label, or modify email, and does not read message
bodies or attachments.

## What it stores

- The OAuth credential (client id/secret and refresh token), encrypted in AWS
  Secrets Manager in the operator's AWS account.
- For each matching message: a page in the AlertMe app containing the sender
  and subject, plus the Gmail message id (used only to avoid duplicate
  alerts). Page data is retained for up to 90 days.

## Sharing

Data is not sold, shared with third parties, or used for advertising or
model training. It is sent only to the account owner's own devices as a push
notification (via AWS SNS and Firebase Cloud Messaging).

Use of information received from Google APIs adheres to the
[Google API Services User Data Policy](https://developers.google.com/terms/api-services-user-data-policy),
including the Limited Use requirements.

## Revoking access

Remove access at any time at <https://myaccount.google.com/permissions>.
Revoking stops the automation immediately.

## Contact

Open an issue at <https://github.com/dgarwin/Alertme/issues>.
