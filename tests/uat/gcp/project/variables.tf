# List of variables which can be provided ar runtime to override the specified defaults 

variable "project_id" {
  description = "GCP Project ID"
  type        = string
  default     = "nv-dgxck8s-20250306"
}

variable "git_repo" {
  description = "GitHub Repo"
  type        = string
  default     = "NVIDIA/NVSentinel"
}
