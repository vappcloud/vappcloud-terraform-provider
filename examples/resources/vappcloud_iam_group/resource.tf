resource "vappcloud_iam_group" "operators" {
  name       = "operators"
  member_ids = var.operator_principal_ids
}

variable "operator_principal_ids" {
  type    = set(string)
  default = []
}
