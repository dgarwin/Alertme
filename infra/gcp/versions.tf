terraform {
  required_version = ">= 1.5.0" # import {} blocks

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }
    google-beta = {
      source  = "hashicorp/google-beta"
      version = "~> 6.0"
    }
  }

  # State is local (infra/gcp/terraform.tfstate, gitignored). It holds no
  # secrets: service-account keys are deliberately created out of band.
}

provider "google" {
  project = var.project_id
  # Auth: GOOGLE_OAUTH_ACCESS_TOKEN or Application Default Credentials.
  # Bill API quota to the project (needed for firebase.googleapis.com with
  # user credentials).
  user_project_override = true
  billing_project       = var.project_id
}

# Firebase resources are only in the beta provider.
provider "google-beta" {
  project               = var.project_id
  user_project_override = true
  billing_project       = var.project_id
}
