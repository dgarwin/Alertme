output "android_app_id" {
  value = google_firebase_android_app.alertme.app_id
}

output "sns_fcm_service_account" {
  value = google_service_account.sns_fcm_push.email
}
