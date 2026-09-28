resource "gezor_sso_domain" "acme" {
  domain      = "acme.com"
  provider_id = gezor_sso_provider.entra.id
}

output "dns_txt" {
  value = "${gezor_sso_domain.acme.txt_host} TXT ${gezor_sso_domain.acme.txt_record}"
}
