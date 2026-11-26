#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${DIST_DIR:-${ROOT_DIR}/dist}"
OUTPUT_ZIP="cidr-api-function-url.zip"

mkdir -p "$DIST_DIR"
DIST_DIR="$(cd -- "$DIST_DIR" && pwd)"
PACKAGE_DIR="$(mktemp -d "$DIST_DIR/.scf-package.XXXXXXXX")"
trap 'rm -rf -- "$PACKAGE_DIR"' EXIT

cd "$ROOT_DIR"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$PACKAGE_DIR/cidr-api" .
cp scf_bootstrap china_city_cidrs.compact.json "$PACKAGE_DIR/"
chmod 755 "$PACKAGE_DIR/cidr-api" "$PACKAGE_DIR/scf_bootstrap"

(
  cd "$PACKAGE_DIR"
  zip -q -X "$OUTPUT_ZIP" cidr-api scf_bootstrap china_city_cidrs.compact.json
)

mv "$PACKAGE_DIR/cidr-api" "$PACKAGE_DIR/scf_bootstrap" "$PACKAGE_DIR/china_city_cidrs.compact.json" "$DIST_DIR/"
mv "$PACKAGE_DIR/$OUTPUT_ZIP" "$DIST_DIR/$OUTPUT_ZIP"
echo "Function URL package ready: $DIST_DIR/$OUTPUT_ZIP"
