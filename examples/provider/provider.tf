terraform {
  required_version = ">= 1.5.0"
  required_providers {
    vappcloud = {
      source  = "vappcloud/vappcloud"
      version = "~> 1.0"
    }
  }
}

provider "vappcloud" {}
