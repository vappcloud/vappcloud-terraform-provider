resource "vappcloud_iam_policy" "readonly_vmm" {
  name        = "readonly-vmm"
  description = "Allow operators to inspect VMM state"
  document = jsonencode({
    Version = "2026-08-01"
    Statement = [{
      Effect   = "Allow"
      Action   = ["vmm:Get", "vmm:List"]
      Resource = "*"
    }]
  })
}
