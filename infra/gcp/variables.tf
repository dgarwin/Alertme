variable "project_id" {
  description = "Pre-existing GCP project hosting Firebase for AlertMe. The project itself is NOT managed here."
  type        = string
  default     = "claude-code-484803"
}

variable "android_package_name" {
  type    = string
  default = "com.alertme.alertme"
}
