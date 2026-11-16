# AlertMe's Google Cloud / Firebase footprint.
#
# The GCP project (var.project_id) pre-exists and is shared with unrelated
# work: we never manage the project resource itself, unrelated services, or
# unrelated IAM. Only the resources below are owned here. Everything was
# first created by hand and adopted via the import {} blocks in imports.tf.
#
# Service-account keys are NOT managed here (they would land in state in
# plaintext). See README.md "Rotating the FCM credential".

locals {
  apis = toset([
    "firebase.googleapis.com",
    "fcm.googleapis.com",
    "gmail.googleapis.com", # mailwatch: read-only Gmail via OAuth
    "iam.googleapis.com",
  ])
}

resource "google_project_service" "apis" {
  for_each = local.apis
  project  = var.project_id
  service  = each.value

  # The project is shared: never turn APIs off on destroy.
  disable_on_destroy         = false
  disable_dependent_services = false
}

resource "google_firebase_project" "this" {
  provider   = google-beta
  project    = var.project_id
  depends_on = [google_project_service.apis]
}

resource "google_firebase_android_app" "alertme" {
  provider     = google-beta
  project      = var.project_id
  display_name = "AlertMe"
  package_name = var.android_package_name

  # Removing this from Terraform must not delete the live app (and its FCM
  # registrations); just forget it.
  deletion_policy = "ABANDON"

  depends_on = [google_firebase_project.this]
}

# Identity AWS SNS uses to call FCM HTTP v1 on our behalf. Its JSON key is
# stored in AWS Secrets Manager (alertme/fcm-service-account) and loaded into
# the CDK-managed SNS platform application.
resource "google_service_account" "sns_fcm_push" {
  project      = var.project_id
  account_id   = "sns-fcm-push"
  display_name = "AWS SNS FCM push (AlertMe)"
}

# Additive member grant only — never _policy/_binding on a shared project.
resource "google_project_iam_member" "sns_fcm_push_fcm_admin" {
  project = var.project_id
  role    = "roles/firebasecloudmessaging.admin"
  member  = "serviceAccount:${google_service_account.sns_fcm_push.email}"
}
