resource "vappcloud_iam_policy_version" "candidate" {
  policy_id      = vappcloud_iam_policy.readonly_vmm.id
  set_as_default = false
  document = jsonencode({
    Version = "2026-08-01"
    Statement = [{
      Effect   = "Allow"
      Action   = ["vmm:Get", "vmm:List", "vmm:Metrics"]
      Resource = "*"
    }]
  })
}
