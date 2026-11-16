#!/usr/bin/env python3
"""One-time setup for the mailwatch lambda: get a read-only Gmail refresh
token and store it in AWS Secrets Manager.

Usage:
    python3 tools/gmail-oauth-setup.py ~/Downloads/client_secret_XXXX.json

The JSON is the "Desktop app" OAuth client downloaded from the Google Cloud
console (see README "Mail watch"). The script:
  1. opens Google's consent page in your browser (scope: gmail.readonly),
  2. receives the authorization code on a loopback port (PKCE),
  3. exchanges it for a refresh token and checks it by reading your Gmail
     profile (prints only the email address),
  4. writes {client_id, client_secret, refresh_token} to the Secrets Manager
     secret (default alertme/gmail-oauth, region us-east-2) via the AWS CLI.

No token is ever printed or left on disk. Re-run it any time to rotate the
token (e.g. after revoking access, or if the alarm reports invalid_grant).
Requires: python3 (stdlib only) and an authenticated AWS CLI.
"""

import argparse
import base64
import hashlib
import http.server
import json
import os
import secrets
import subprocess
import sys
import tempfile
import threading
import urllib.parse
import urllib.request
import webbrowser

SCOPE = "https://www.googleapis.com/auth/gmail.readonly"
AUTH_URL = "https://accounts.google.com/o/oauth2/v2/auth"
TOKEN_URL = "https://oauth2.googleapis.com/token"
PROFILE_URL = "https://gmail.googleapis.com/gmail/v1/users/me/profile"


def load_client(path):
    with open(os.path.expanduser(path)) as f:
        data = json.load(f)
    c = data.get("installed") or data.get("web")
    if not c or "client_id" not in c or "client_secret" not in c:
        sys.exit("That file doesn't look like an OAuth client JSON (expected an 'installed' block).")
    if "web" in data:
        print("Note: this is a 'Web application' client; a 'Desktop app' client is recommended.")
    return c["client_id"], c["client_secret"]


def get_code(client_id):
    verifier = base64.urlsafe_b64encode(secrets.token_bytes(48)).rstrip(b"=").decode()
    challenge = base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).rstrip(b"=").decode()
    state = secrets.token_urlsafe(24)
    result = {}

    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            q = urllib.parse.parse_qs(urllib.parse.urlparse(self.path).query)
            if q.get("state", [None])[0] != state:
                self.send_response(400); self.end_headers()
                self.wfile.write(b"State mismatch; ignore this tab."); return
            result["code"] = q.get("code", [None])[0]
            result["error"] = q.get("error", [None])[0]
            self.send_response(200)
            self.send_header("Content-Type", "text/html")
            self.end_headers()
            msg = "AlertMe: done. You can close this tab." if result["code"] else "AlertMe: authorization failed."
            self.wfile.write(f"<html><body><h2>{msg}</h2></body></html>".encode())

        def log_message(self, *a):
            pass

    server = http.server.HTTPServer(("127.0.0.1", 0), Handler)
    redirect = f"http://127.0.0.1:{server.server_port}/"
    params = {
        "client_id": client_id,
        "redirect_uri": redirect,
        "response_type": "code",
        "scope": SCOPE,
        "access_type": "offline",
        "prompt": "consent",  # always return a refresh token
        "code_challenge": challenge,
        "code_challenge_method": "S256",
        "state": state,
    }
    url = AUTH_URL + "?" + urllib.parse.urlencode(params)
    t = threading.Thread(target=server.handle_request, daemon=True)
    t.start()
    print("Opening Google's consent page in your browser...")
    print("If it doesn't open, paste this URL into a browser:\n  " + url + "\n")
    webbrowser.open(url)
    t.join(timeout=300)
    server.server_close()
    if not result.get("code"):
        sys.exit(f"No authorization code received ({result.get('error') or 'timed out'}).")
    return result["code"], verifier, redirect


def post_form(url, data):
    req = urllib.request.Request(url, data=urllib.parse.urlencode(data).encode(), method="POST")
    try:
        with urllib.request.urlopen(req, timeout=20) as r:
            return json.load(r)
    except urllib.error.HTTPError as e:
        body = json.loads(e.read() or b"{}")
        sys.exit(f"Token exchange failed: {body.get('error')} {body.get('error_description', '')}")


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("client_json", help="OAuth client JSON downloaded from the Google Cloud console")
    ap.add_argument("--secret", default="alertme/gmail-oauth")
    ap.add_argument("--region", default="us-east-2")
    args = ap.parse_args()

    client_id, client_secret = load_client(args.client_json)
    code, verifier, redirect = get_code(client_id)
    tok = post_form(TOKEN_URL, {
        "grant_type": "authorization_code", "code": code, "code_verifier": verifier,
        "client_id": client_id, "client_secret": client_secret, "redirect_uri": redirect,
    })
    if "refresh_token" not in tok:
        sys.exit("Google returned no refresh token. Revoke the app at https://myaccount.google.com/permissions and re-run.")
    if SCOPE not in tok.get("scope", ""):
        sys.exit("Read-only Gmail access wasn't granted (did you untick it on the consent page?). Re-run.")

    req = urllib.request.Request(PROFILE_URL, headers={"Authorization": "Bearer " + tok["access_token"]})
    with urllib.request.urlopen(req, timeout=20) as r:
        print("Verified read-only access to:", json.load(r)["emailAddress"])

    payload = json.dumps({"client_id": client_id, "client_secret": client_secret,
                          "refresh_token": tok["refresh_token"]})
    fd, path = tempfile.mkstemp(prefix="gmail-oauth-", suffix=".json")
    try:
        with os.fdopen(fd, "w") as f:
            f.write(payload)
        os.chmod(path, 0o600)
        base = ["aws", "secretsmanager", "--region", args.region]
        exists = subprocess.run(base + ["describe-secret", "--secret-id", args.secret],
                                capture_output=True).returncode == 0
        if exists:
            cmd = base + ["put-secret-value", "--secret-id", args.secret, "--secret-string", "file://" + path]
        else:
            cmd = base + ["create-secret", "--name", args.secret, "--description",
                          "AlertMe mailwatch: Gmail read-only OAuth credential", "--secret-string", "file://" + path]
        r = subprocess.run(cmd, capture_output=True, text=True)
        if r.returncode != 0:
            sys.exit("Saving to Secrets Manager failed: " + r.stderr.strip())
    finally:
        os.remove(path)
    print(f"Saved to Secrets Manager secret '{args.secret}' ({args.region}).")
    print("The mailwatch job will pick it up on its next run (within 10 minutes).")
    print("You can now delete the downloaded client JSON.")


if __name__ == "__main__":
    main()
