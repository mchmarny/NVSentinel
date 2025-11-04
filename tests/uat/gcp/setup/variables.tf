# List of variables which can be provided ar runtime to override the specified defaults 

variable "project_id" {
  description = "GCP Project ID"
  type        = string
  default     = "proj-dgxc-nvsentinel"
}

variable "registry_location" {
  description = "Location of the Artifact Registry"
  type        = string
  default     = "us"
}

variable "registry_name" {
  description = "Name (ID) of the Artifact Registry"
  type        = string
  default     = "nvsentinel"
}

variable "git_repo" {
  description = "GitHub Repo"
  type        = string
  default     = "NVIDIA/NVSentinel"
}
