resource "vappcloud_compute_instance" "worker" {
  project_id          = vappcloud_project.production.id
  device_id           = vappcloud_device.worker.id
  cloud_connection_id = "cloud_connection_example"
  region              = "us-east-1"
  size                = "standard-4"
  image               = "ubuntu-24.04-arm64"
  name                = "worker-01"

  timeouts {
    create = "30m"
    update = "30m"
    delete = "30m"
  }
}
