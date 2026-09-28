# Security policy

## Reporting a vulnerability

Please do not open a public issue for security problems. Report them privately through [GitHub's private vulnerability reporting](https://github.com/Gezor-Labs/terraform-provider-gezor/security/advisories/new).

## Supported versions

Security fixes are released for the latest minor version.

## Release integrity

Every release is signed with the Gezor Labs provider signing key:

```
Key ID:      2C1F2396EEDC4BE7
Fingerprint: F090 976C 2AE8 E8AA 3366  141E 2C1F 2396 EEDC 4BE7
```

The public key is in [`signing-key.asc`](signing-key.asc). Terraform checks the signature on `terraform init`.

## API tokens

The provider only needs a Gezor service account token (`gzr_sa_...`). Pass it through the `GEZOR_TOKEN` environment variable, give the service account the smallest role that works, and set an expiry. Tokens can never manage people, service accounts or other tokens.
