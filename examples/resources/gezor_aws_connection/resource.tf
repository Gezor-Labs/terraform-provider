data "gezor_aws_cloudformation" "this" {}

resource "aws_cloudformation_stack" "gezor" {
  name          = "gezor-access"
  template_body = data.gezor_aws_cloudformation.this.template_body
  capabilities  = ["CAPABILITY_NAMED_IAM"]
}

resource "gezor_aws_connection" "this" {
  auth_mode = "assume_role"
  role_arn  = aws_cloudformation_stack.gezor.outputs["RoleArn"]
}
