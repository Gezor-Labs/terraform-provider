#!/usr/bin/env bash
# Publish a GoReleaser release to an HCP Terraform or Terraform Enterprise private registry.
#
# Usage: scripts/publish-private-registry.sh <dist-dir> <version>
#
#   TFC_TOKEN         Team or user API token with "Manage Private Registry" (required)
#   TFC_ORGANIZATION  Organization name, also the provider namespace (required)
#   TFC_HOSTNAME      app.terraform.io (default) or your Terraform Enterprise host
#
# <dist-dir> must contain the release files: *_SHA256SUMS, *_SHA256SUMS.sig and the *.zip archives.
# Safe to re-run: existing providers, keys, versions and platforms are reused and only
# missing files are uploaded.
set -euo pipefail

DIST="${1:?dist directory}"
VERSION="${2:?version, e.g. 0.1.0}"
VERSION="${VERSION#v}"
: "${TFC_TOKEN:?TFC_TOKEN is required}"
: "${TFC_ORGANIZATION:?TFC_ORGANIZATION is required}"
HOST="${TFC_HOSTNAME:-app.terraform.io}"
ORG="$TFC_ORGANIZATION"
NAME="gezor"
KEY_FILE="$(cd "$(dirname "$0")/.." && pwd)/signing-key.asc"
API="https://${HOST}/api/v2/organizations/${ORG}/registry-providers/private/${ORG}/${NAME}"

command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }

# api METHOD URL [BODY] -> prints the response body, returns 0 on 2xx, 4 on 404, 1 otherwise.
api() {
  local method="$1" url="$2" body="${3:-}" out code
  out="$(mktemp)"
  if [[ -n "$body" ]]; then
    code="$(curl -sS -o "$out" -w '%{http_code}' -X "$method" \
      -H "Authorization: Bearer ${TFC_TOKEN}" -H "Content-Type: application/vnd.api+json" \
      --data "$body" "$url")"
  else
    code="$(curl -sS -o "$out" -w '%{http_code}' -X "$method" \
      -H "Authorization: Bearer ${TFC_TOKEN}" "$url")"
  fi
  cat "$out"; rm -f "$out"
  case "$code" in
    2??) return 0 ;;
    404) return 4 ;;
    *) echo >&2; echo "HTTP ${code} from ${method} ${url}" >&2; return 1 ;;
  esac
}

upload() {
  local file="$1" url="$2"
  curl -sS --fail -T "$file" "$url" >/dev/null
  echo "  uploaded $(basename "$file")"
}

SUMS="$(ls "$DIST"/*_"${VERSION}"_SHA256SUMS)"
SIG="${SUMS}.sig"
[[ -f "$SIG" ]] || { echo "missing ${SIG}" >&2; exit 1; }

echo "==> provider ${ORG}/${NAME} on ${HOST}"
if ! api GET "$API" >/dev/null 2>&1; then
  api POST "https://${HOST}/api/v2/organizations/${ORG}/registry-providers" \
    "$(jq -n --arg n "$NAME" --arg o "$ORG" \
      '{data:{type:"registry-providers",attributes:{name:$n,namespace:$o,"registry-name":"private"}}}')" >/dev/null
  echo "  created"
fi

echo "==> signing key"
KEY_ID="$(gpg --show-keys --with-colons "$KEY_FILE" 2>/dev/null | awk -F: '/^pub/{print $5; exit}')"
[[ -n "$KEY_ID" ]] || { echo "cannot read key id from ${KEY_FILE}" >&2; exit 1; }
if ! api GET "https://${HOST}/api/registry/private/v2/gpg-keys?filter%5Bnamespace%5D=${ORG}" \
  | jq -e --arg k "$KEY_ID" '.data[] | select(.attributes["key-id"] == $k)' >/dev/null; then
  api POST "https://${HOST}/api/registry/private/v2/gpg-keys" \
    "$(jq -n --arg o "$ORG" --rawfile a "$KEY_FILE" \
      '{data:{type:"gpg-keys",attributes:{namespace:$o,"ascii-armor":$a}}}')" >/dev/null
  echo "  added ${KEY_ID}"
else
  echo "  ${KEY_ID} already present"
fi

echo "==> version ${VERSION}"
if ! VER="$(api GET "${API}/versions/${VERSION}" 2>/dev/null)"; then
  VER="$(api POST "${API}/versions" \
    "$(jq -n --arg v "$VERSION" --arg k "$KEY_ID" \
      '{data:{type:"registry-provider-versions",attributes:{version:$v,"key-id":$k,protocols:["6.0"]}}}')")"
  echo "  created"
fi
if [[ "$(jq -r '.data.attributes["shasums-uploaded"]' <<<"$VER")" != "true" ]]; then
  upload "$SUMS" "$(jq -r '.data.links["shasums-upload"]' <<<"$VER")"
fi
if [[ "$(jq -r '.data.attributes["shasums-sig-uploaded"]' <<<"$VER")" != "true" ]]; then
  upload "$SIG" "$(jq -r '.data.links["shasums-sig-upload"]' <<<"$VER")"
fi

echo "==> platforms"
for zip in "$DIST"/terraform-provider-${NAME}_"${VERSION}"_*.zip; do
  file="$(basename "$zip")"
  platform="${file#terraform-provider-${NAME}_${VERSION}_}"
  platform="${platform%.zip}"
  os="${platform%%_*}"
  arch="${platform#*_}"
  sum="$(awk -v f="$file" '$2 == f {print $1}' "$SUMS")"
  [[ -n "$sum" ]] || { echo "${file} is not in ${SUMS}" >&2; exit 1; }
  if ! PLAT="$(api GET "${API}/versions/${VERSION}/platforms/${os}/${arch}" 2>/dev/null)"; then
    PLAT="$(api POST "${API}/versions/${VERSION}/platforms" \
      "$(jq -n --arg os "$os" --arg arch "$arch" --arg s "$sum" --arg f "$file" \
        '{data:{type:"registry-provider-version-platforms",attributes:{os:$os,arch:$arch,shasum:$s,filename:$f}}}')")"
  fi
  if [[ "$(jq -r '.data.attributes["provider-binary-uploaded"]' <<<"$PLAT")" != "true" ]]; then
    upload "$zip" "$(jq -r '.data.links["provider-binary-upload"]' <<<"$PLAT")"
  else
    echo "  ${os}_${arch} already uploaded"
  fi
done

echo "Published ${HOST}/${ORG}/${NAME} ${VERSION}"
