##############################################################################
# Provider: fortinetdev/fortios
# https://registry.terraform.io/providers/fortinetdev/fortios/latest/docs
#
# NOTE: nothing in this file is CR-driven (no <FGT_FOR_STR> tags) - it only
# wires up provider credentials, which come from Terraform variables /
# environment, not from the FortigateConfig CR.
##############################################################################

terraform {
  required_version = ">= 1.3.0"
  required_providers {
    fortios = {
      source  = "fortinetdev/fortios"
      version = ">= 1.24.0"
    }
  }
}

variable "fgt_hostname" {
  description = "Management IP/hostname of the FortiGate"
  type        = string
}

variable "fgt_token" {
  description = "REST API token generated on the FortiGate (recommended over user/pass)"
  type        = string
  sensitive   = true
}

variable "fgt_insecure" {
  description = "Set to true only for lab/testing to skip TLS certificate validation"
  type        = bool
  default     = false
}

provider "fortios" {
  hostname = var.fgt_hostname
  token    = var.fgt_token
  insecure = var.fgt_insecure ? "true" : "false"
}
