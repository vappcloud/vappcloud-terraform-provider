resource "vappcloud_iam_policy_attachment" "operators_readonly_vmm" {
  policy_id   = vappcloud_iam_policy.readonly_vmm.id
  target_type = "group"
  target_id   = vappcloud_iam_group.operators.id
}
