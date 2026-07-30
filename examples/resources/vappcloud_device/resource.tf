resource "vappcloud_device" "worker" {
  project_id = vappcloud_project.production.id
  name       = "worker-01"
}
