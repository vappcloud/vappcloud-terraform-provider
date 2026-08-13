resource "vappcloud_device" "host" {
  account_id = vappcloud_account.example.id
  name       = "worker-1"
}

resource "vappcloud_vmm" "secondary" {
  account_id           = vappcloud_account.example.id
  device_id            = vappcloud_device.host.id
  name                 = "application-pool"
  cpu_cores            = 4
  memory_mb            = 8192
  instance_profile_arn = "arn:vapp:iam::123:instance-profile/application-pool"
  deletion_protection  = true
  retain_disk          = false
}
