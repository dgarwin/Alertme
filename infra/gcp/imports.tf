# Adoption of resources originally created by hand (Terraform >= 1.5 import
# blocks). Harmless to keep after the first apply: importing an
# already-managed resource is a no-op. Written out explicitly (no for_each)
# so Terraform 1.5/1.6 work too.

import {
  to = google_project_service.apis["firebase.googleapis.com"]
  id = "claude-code-484803/firebase.googleapis.com"
}

import {
  to = google_project_service.apis["fcm.googleapis.com"]
  id = "claude-code-484803/fcm.googleapis.com"
}

import {
  to = google_project_service.apis["iam.googleapis.com"]
  id = "claude-code-484803/iam.googleapis.com"
}

import {
  provider = google-beta
  to       = google_firebase_project.this
  id       = "projects/claude-code-484803"
}

import {
  provider = google-beta
  to       = google_firebase_android_app.alertme
  id       = "projects/claude-code-484803/androidApps/1:120140834853:android:a725152c1a4f3284d237cc"
}

import {
  to = google_service_account.sns_fcm_push
  id = "projects/claude-code-484803/serviceAccounts/sns-fcm-push@claude-code-484803.iam.gserviceaccount.com"
}

import {
  to = google_project_iam_member.sns_fcm_push_fcm_admin
  id = "claude-code-484803 roles/firebasecloudmessaging.admin serviceAccount:sns-fcm-push@claude-code-484803.iam.gserviceaccount.com"
}
