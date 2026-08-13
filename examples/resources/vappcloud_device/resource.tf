resource "vappcloud_device" "worker" {
  account_id = vappcloud_account.production.id
  name       = "worker-01"
}
