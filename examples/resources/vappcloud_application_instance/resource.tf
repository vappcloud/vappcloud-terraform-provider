resource "vappcloud_application_instance" "nginx" {
  project_id  = vappcloud_project.example.id
  name        = "nginx"
  description = "Example marketplace deployment"

  source = {
    kind                       = "marketplace"
    marketplace_application_id = data.vappcloud_marketplace_applications.catalog.items[0].id
    marketplace_version_id     = data.vappcloud_marketplace_versions.nginx.items[0].id
  }

  placement = [
    {
      vmm_id       = vappcloud_vmm.secondary.id
      replica_count = 1
    }
  ]
}
