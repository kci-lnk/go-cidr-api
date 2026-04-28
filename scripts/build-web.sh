#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${ROOT_DIR}/dist"
OUTPUT_ZIP="${DIST_DIR}/cidr-api-function-url.zip"

mkdir -p "${DIST_DIR}"
rm -rf "${DIST_DIR}/cidr-api" "${DIST_DIR}/scf_bootstrap" "${DIST_DIR}/china_city_cidrs.compact.json" "${OUTPUT_ZIP}" "${DIST_DIR}/main" "${DIST_DIR}/cidr-api.zip"

pushd "${ROOT_DIR}" >/dev/null
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o "${DIST_DIR}/cidr-api" .
cp "${ROOT_DIR}/scf_bootstrap" "${DIST_DIR}/scf_bootstrap"
cp "${ROOT_DIR}/china_city_cidrs.compact.json" "${DIST_DIR}/china_city_cidrs.compact.json"
chmod 755 "${DIST_DIR}/cidr-api" "${DIST_DIR}/scf_bootstrap"
(
  cd "${DIST_DIR}"
  zip -q "$(basename "${OUTPUT_ZIP}")" cidr-api scf_bootstrap china_city_cidrs.compact.json
)
popd >/dev/null

echo "Function URL package ready: ${OUTPUT_ZIP}"
