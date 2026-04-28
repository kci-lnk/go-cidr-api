#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
DOCKER_DIR="${ROOT_DIR}/deploy/docker"
DOCKERFILE="${DOCKER_DIR}/Dockerfile"
DEFAULT_ENV_FILE="${DOCKER_DIR}/.env"
FALLBACK_ENV_FILE="${DOCKER_DIR}/.env.example"
VERSION_FILE="${CIDR_API_DOCKER_VERSION_FILE:-${ROOT_DIR}/VERSION}"
CACHE_ROOT="${CIDR_API_DOCKER_CACHE_DIR:-${HOME}/.cache/go-cidr-api-buildx}"
MANAGED_BUILDER_NAME="${CIDR_API_DOCKER_MANAGED_BUILDER:-go-cidr-api-buildx}"
USER_BUILDER_NAME="${CIDR_API_DOCKER_BUILDER:-}"
PROXY_HOST_ALIAS="${CIDR_API_DOCKER_PROXY_HOST_ALIAS:-host.docker.internal}"
MANIFEST_RETRY_ATTEMPTS="${CIDR_API_DOCKER_MANIFEST_RETRY_ATTEMPTS:-5}"
MANIFEST_RETRY_DELAY="${CIDR_API_DOCKER_MANIFEST_RETRY_DELAY:-5}"

EFFECTIVE_BUILDER_NAME=""
EFFECTIVE_BUILDER_DRIVER=""
BUILD_HTTP_PROXY=""
BUILD_HTTPS_PROXY=""
BUILD_ALL_PROXY=""
BUILD_NO_PROXY=""
BUILD_PROXY_ENABLED=0

log() {
  echo "[go-cidr-api-docker] $*"
}

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

retry_delay_for_attempt() {
  local attempt="$1"
  echo $((MANIFEST_RETRY_DELAY * attempt))
}

require_cmd() {
  local cmd="$1"
  command -v "${cmd}" >/dev/null 2>&1 || fail "missing required command: ${cmd}"
}

resolve_env_file() {
  if [ -n "${CIDR_API_DOCKER_ENV_FILE:-}" ]; then
    echo "${CIDR_API_DOCKER_ENV_FILE}"
    return 0
  fi

  if [ -f "${DEFAULT_ENV_FILE}" ]; then
    echo "${DEFAULT_ENV_FILE}"
    return 0
  fi

  echo "${FALLBACK_ENV_FILE}"
}

ENV_FILE="$(resolve_env_file)"

read_env_value() {
  local key="$1"
  local default_value="${2:-}"
  local value

  value="$(
    awk -v key="${key}" '
      /^[[:space:]]*#/ { next }
      index($0, "=") == 0 { next }
      {
        current_key = substr($0, 1, index($0, "=") - 1)
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", current_key)
        if (current_key == key) {
          value = substr($0, index($0, "=") + 1)
          gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
          gsub(/\r$/, "", value)
          print value
          exit
        }
      }
    ' "${ENV_FILE}" 2>/dev/null || true
  )"

  if [ -n "${value}" ]; then
    echo "${value}"
    return 0
  fi

  echo "${default_value}"
}

read_proxy_value() {
  local explicit_var="$1"
  local upper_var="$2"
  local lower_var="$3"
  local value="${!explicit_var:-}"

  if [ -n "${value}" ]; then
    printf '%s' "${value}"
    return 0
  fi

  value="${!upper_var:-}"
  if [ -n "${value}" ]; then
    printf '%s' "${value}"
    return 0
  fi

  value="${!lower_var:-}"
  printf '%s' "${value}"
}

normalize_proxy_for_container() {
  local value="$1"

  if [ -z "${value}" ]; then
    printf '%s' ""
    return 0
  fi

  printf '%s' "${value}" | sed -E \
    -e "s#://(([^/@]+@)?)(127\\.0\\.0\\.1|localhost)([:/]|$)#://\\1${PROXY_HOST_ALIAS}\\4#" \
    -e "s#^(([^/@]+@)?)(127\\.0\\.0\\.1|localhost)([:/]|$)#\\1${PROXY_HOST_ALIAS}\\4#" \
    -e "s#://(([^/@]+@)?)\\[::1\\]([:/]|$)#://\\1${PROXY_HOST_ALIAS}\\3#" \
    -e "s#^(([^/@]+@)?)\\[::1\\]([:/]|$)#\\1${PROXY_HOST_ALIAS}\\3#"
}

configure_build_proxy() {
  local raw_http_proxy
  local raw_https_proxy
  local raw_all_proxy
  local raw_no_proxy

  raw_http_proxy="$(read_proxy_value "CIDR_API_DOCKER_HTTP_PROXY" "HTTP_PROXY" "http_proxy")"
  raw_https_proxy="$(read_proxy_value "CIDR_API_DOCKER_HTTPS_PROXY" "HTTPS_PROXY" "https_proxy")"
  raw_all_proxy="$(read_proxy_value "CIDR_API_DOCKER_ALL_PROXY" "ALL_PROXY" "all_proxy")"
  raw_no_proxy="$(read_proxy_value "CIDR_API_DOCKER_NO_PROXY" "NO_PROXY" "no_proxy")"

  if [ -z "${raw_https_proxy}" ] && [ -n "${raw_http_proxy}" ]; then
    raw_https_proxy="${raw_http_proxy}"
  fi

  BUILD_HTTP_PROXY="$(normalize_proxy_for_container "${raw_http_proxy}")"
  BUILD_HTTPS_PROXY="$(normalize_proxy_for_container "${raw_https_proxy}")"
  BUILD_ALL_PROXY="$(normalize_proxy_for_container "${raw_all_proxy}")"
  BUILD_NO_PROXY="$(normalize_proxy_for_container "${raw_no_proxy}")"
  BUILD_PROXY_ENABLED=0

  if [ -n "${BUILD_HTTP_PROXY}${BUILD_HTTPS_PROXY}${BUILD_ALL_PROXY}${BUILD_NO_PROXY}" ]; then
    BUILD_PROXY_ENABLED=1
  fi
}

buildkit_container_name() {
  local builder_name="$1"
  printf 'buildx_buildkit_%s0' "${builder_name}"
}

builder_proxy_env_missing() {
  local builder_name="$1"
  local container_name
  local env_output

  [ "${BUILD_PROXY_ENABLED}" = "1" ] || return 1

  container_name="$(buildkit_container_name "${builder_name}")"
  env_output="$(docker inspect "${container_name}" --format '{{range .Config.Env}}{{println .}}{{end}}' 2>/dev/null || true)"

  if [ -z "${env_output}" ]; then
    return 0
  fi

  if [ -n "${BUILD_HTTP_PROXY}" ] && ! printf '%s\n' "${env_output}" | grep -Fxq "http_proxy=${BUILD_HTTP_PROXY}"; then
    return 0
  fi
  if [ -n "${BUILD_HTTP_PROXY}" ] && ! printf '%s\n' "${env_output}" | grep -Fxq "HTTP_PROXY=${BUILD_HTTP_PROXY}"; then
    return 0
  fi
  if [ -n "${BUILD_HTTPS_PROXY}" ] && ! printf '%s\n' "${env_output}" | grep -Fxq "https_proxy=${BUILD_HTTPS_PROXY}"; then
    return 0
  fi
  if [ -n "${BUILD_HTTPS_PROXY}" ] && ! printf '%s\n' "${env_output}" | grep -Fxq "HTTPS_PROXY=${BUILD_HTTPS_PROXY}"; then
    return 0
  fi
  if [ -n "${BUILD_ALL_PROXY}" ] && ! printf '%s\n' "${env_output}" | grep -Fxq "all_proxy=${BUILD_ALL_PROXY}"; then
    return 0
  fi
  if [ -n "${BUILD_ALL_PROXY}" ] && ! printf '%s\n' "${env_output}" | grep -Fxq "ALL_PROXY=${BUILD_ALL_PROXY}"; then
    return 0
  fi
  if [ -n "${BUILD_NO_PROXY}" ] && ! printf '%s\n' "${env_output}" | grep -Fxq "no_proxy=${BUILD_NO_PROXY}"; then
    return 0
  fi
  if [ -n "${BUILD_NO_PROXY}" ] && ! printf '%s\n' "${env_output}" | grep -Fxq "NO_PROXY=${BUILD_NO_PROXY}"; then
    return 0
  fi

  return 1
}

read_version() {
  local version

  if [ -n "${CIDR_API_DOCKER_IMAGE_TAG:-}" ]; then
    version="${CIDR_API_DOCKER_IMAGE_TAG}"
  else
    [ -f "${VERSION_FILE}" ] || fail "missing version file: ${VERSION_FILE}"
    version="$(awk 'NF && $1 !~ /^#/ { print $1; exit }' "${VERSION_FILE}")"
  fi

  [ -n "${version}" ] || fail "empty Docker image version"
  case "${version}" in
    [A-Za-z0-9_]*)
      ;;
    *)
      fail "invalid Docker tag ${version}; use letters, numbers, underscore, dot, or dash"
      ;;
  esac

  case "${version}" in
    *[!A-Za-z0-9_.-]*)
      fail "invalid Docker tag ${version}; use letters, numbers, underscore, dot, or dash"
      ;;
  esac

  if [ "${#version}" -gt 128 ]; then
    fail "Docker tag is too long: ${version}"
  fi

  echo "${version}"
}

resolve_image_repo() {
  local image_repo

  if [ -n "${CIDR_API_DOCKER_IMAGE_REPO:-}" ]; then
    image_repo="${CIDR_API_DOCKER_IMAGE_REPO}"
  elif [ -n "${DOCKER_IMAGE_REPO:-}" ]; then
    image_repo="${DOCKER_IMAGE_REPO}"
  else
    image_repo="$(read_env_value "CIDR_API_DOCKER_IMAGE_REPO" "")"
  fi

  [ -n "${image_repo}" ] || fail "CIDR_API_DOCKER_IMAGE_REPO is required, for example kcilnk/go-cidr-api"

  case "${image_repo}" in
    *:*)
      fail "CIDR_API_DOCKER_IMAGE_REPO must not include a tag: ${image_repo}"
      ;;
    */*)
      ;;
    *)
      fail "Docker Hub publish target must include namespace/repo, for example kcilnk/go-cidr-api"
      ;;
  esac

  echo "${image_repo}"
}

normalize_arch() {
  case "$1" in
    amd64 | x86_64)
      echo "amd64"
      ;;
    arm64 | aarch64)
      echo "arm64"
      ;;
    *)
      fail "unsupported architecture: $1"
      ;;
  esac
}

detect_local_arch() {
  normalize_arch "$(uname -m)"
}

resolve_platforms() {
  local raw_platforms

  if [ -n "${CIDR_API_DOCKER_PLATFORMS:-}" ]; then
    raw_platforms="${CIDR_API_DOCKER_PLATFORMS}"
  elif [ -n "${DOCKER_PLATFORMS:-}" ]; then
    raw_platforms="${DOCKER_PLATFORMS}"
  else
    raw_platforms="$(read_env_value "CIDR_API_DOCKER_PLATFORMS" "linux/amd64,linux/arm64")"
  fi

  echo "${raw_platforms}" | tr -d '[:space:]'
}

platform_to_arch() {
  case "$1" in
    linux/amd64 | amd64)
      echo "amd64"
      ;;
    linux/arm64 | arm64)
      echo "arm64"
      ;;
    *)
      fail "unsupported platform: $1"
      ;;
  esac
}

ensure_buildx_builder() {
  local inspect_output
  local create_args=()

  configure_build_proxy

  if [ -n "${EFFECTIVE_BUILDER_NAME}" ] && [ -n "${EFFECTIVE_BUILDER_DRIVER}" ]; then
    return 0
  fi

  if [ -n "${USER_BUILDER_NAME}" ]; then
    EFFECTIVE_BUILDER_NAME="${USER_BUILDER_NAME}"
    docker buildx inspect "${EFFECTIVE_BUILDER_NAME}" >/dev/null 2>&1 || \
      fail "specified buildx builder not found: ${EFFECTIVE_BUILDER_NAME}"
    if builder_proxy_env_missing "${EFFECTIVE_BUILDER_NAME}"; then
      fail "specified buildx builder ${EFFECTIVE_BUILDER_NAME} is missing proxy env; recreate it with proxy support or unset CIDR_API_DOCKER_BUILDER"
    fi
  else
    EFFECTIVE_BUILDER_NAME="${MANAGED_BUILDER_NAME}"
    if docker buildx inspect "${EFFECTIVE_BUILDER_NAME}" >/dev/null 2>&1 && builder_proxy_env_missing "${EFFECTIVE_BUILDER_NAME}"; then
      log "Recreating buildx builder ${EFFECTIVE_BUILDER_NAME} to apply proxy settings"
      docker buildx rm "${EFFECTIVE_BUILDER_NAME}" >/dev/null
    fi
    if ! docker buildx inspect "${EFFECTIVE_BUILDER_NAME}" >/dev/null 2>&1; then
      log "Creating buildx builder ${EFFECTIVE_BUILDER_NAME}"
      create_args=(--name "${EFFECTIVE_BUILDER_NAME}" --driver docker-container)
      if [ -n "${BUILD_HTTP_PROXY}" ]; then
        create_args+=(--driver-opt "env.http_proxy=${BUILD_HTTP_PROXY}" --driver-opt "env.HTTP_PROXY=${BUILD_HTTP_PROXY}")
      fi
      if [ -n "${BUILD_HTTPS_PROXY}" ]; then
        create_args+=(--driver-opt "env.https_proxy=${BUILD_HTTPS_PROXY}" --driver-opt "env.HTTPS_PROXY=${BUILD_HTTPS_PROXY}")
      fi
      if [ -n "${BUILD_ALL_PROXY}" ]; then
        create_args+=(--driver-opt "env.all_proxy=${BUILD_ALL_PROXY}" --driver-opt "env.ALL_PROXY=${BUILD_ALL_PROXY}")
      fi
      if [ -n "${BUILD_NO_PROXY}" ]; then
        create_args+=(--driver-opt "env.no_proxy=${BUILD_NO_PROXY}" --driver-opt "env.NO_PROXY=${BUILD_NO_PROXY}")
      fi
      docker buildx create "${create_args[@]}" >/dev/null
    fi
  fi

  inspect_output="$(docker buildx inspect --bootstrap "${EFFECTIVE_BUILDER_NAME}")" || \
    fail "failed to bootstrap buildx builder: ${EFFECTIVE_BUILDER_NAME}"
  EFFECTIVE_BUILDER_DRIVER="$(printf '%s\n' "${inspect_output}" | sed -n 's/^Driver:[[:space:]]*//p' | head -n1)"
  [ -n "${EFFECTIVE_BUILDER_DRIVER}" ] || fail "failed to detect buildx driver for ${EFFECTIVE_BUILDER_NAME}"

  log "Using buildx builder ${EFFECTIVE_BUILDER_NAME} (${EFFECTIVE_BUILDER_DRIVER})"
}

finalize_cache_dir() {
  local cache_dir="$1"
  local cache_next="$2"

  if [ -d "${cache_next}" ]; then
    rm -rf "${cache_dir}"
    mv "${cache_next}" "${cache_dir}"
  fi
}

run_buildx_image() {
  local arch="$1"
  local image_ref="$2"
  local output_mode="$3"
  local version="$4"
  local cache_dir="${CACHE_ROOT}/${arch}"
  local cache_next="${cache_dir}-next"
  local build_args=()
  local cache_export_enabled=1

  ensure_buildx_builder

  mkdir -p "${CACHE_ROOT}"
  rm -rf "${cache_next}"

  build_args+=(--builder "${EFFECTIVE_BUILDER_NAME}")

  if [ "${BUILD_PROXY_ENABLED}" = "1" ]; then
    log "Docker build proxy enabled via ${PROXY_HOST_ALIAS}"
    if [ -n "${BUILD_HTTP_PROXY}" ]; then
      build_args+=(--build-arg "HTTP_PROXY=${BUILD_HTTP_PROXY}" --build-arg "http_proxy=${BUILD_HTTP_PROXY}")
    fi
    if [ -n "${BUILD_HTTPS_PROXY}" ]; then
      build_args+=(--build-arg "HTTPS_PROXY=${BUILD_HTTPS_PROXY}" --build-arg "https_proxy=${BUILD_HTTPS_PROXY}")
    fi
    if [ -n "${BUILD_ALL_PROXY}" ]; then
      build_args+=(--build-arg "ALL_PROXY=${BUILD_ALL_PROXY}" --build-arg "all_proxy=${BUILD_ALL_PROXY}")
    fi
    if [ -n "${BUILD_NO_PROXY}" ]; then
      build_args+=(--build-arg "NO_PROXY=${BUILD_NO_PROXY}" --build-arg "no_proxy=${BUILD_NO_PROXY}")
    fi
  fi

  if [ "${EFFECTIVE_BUILDER_DRIVER}" = "docker" ]; then
    cache_export_enabled=0
    log "Builder ${EFFECTIVE_BUILDER_NAME} uses docker driver; skipping local cache export"
  fi

  if [ "${cache_export_enabled}" = "1" ] && [ -d "${cache_dir}" ]; then
    build_args+=(--cache-from "type=local,src=${cache_dir}")
  fi

  case "${output_mode}" in
    load)
      build_args+=(--load)
      ;;
    push)
      build_args+=(--push)
      ;;
    *)
      fail "unsupported buildx output mode: ${output_mode}"
      ;;
  esac

  build_args+=(
    --platform "linux/${arch}"
    --build-arg "APP_VERSION=${version}"
    -f "${DOCKERFILE}"
    -t "${image_ref}"
    "${ROOT_DIR}"
  )

  if [ "${cache_export_enabled}" = "1" ]; then
    build_args=(--cache-to "type=local,dest=${cache_next},mode=max" "${build_args[@]}")
  fi

  log "Building ${image_ref} for linux/${arch} (${output_mode})"
  docker buildx build "${build_args[@]}"

  if [ "${cache_export_enabled}" = "1" ]; then
    finalize_cache_dir "${cache_dir}" "${cache_next}"
  fi
}

create_manifest_tag() {
  local target_ref="$1"
  local attempt
  local delay
  shift

  for attempt in $(seq 1 "${MANIFEST_RETRY_ATTEMPTS}"); do
    log "Creating multi-arch manifest ${target_ref} (attempt ${attempt}/${MANIFEST_RETRY_ATTEMPTS})"
    if docker buildx imagetools create -t "${target_ref}" "$@"; then
      return 0
    fi

    if [ "${attempt}" -lt "${MANIFEST_RETRY_ATTEMPTS}" ]; then
      delay="$(retry_delay_for_attempt "${attempt}")"
      log "Manifest create failed for ${target_ref}; retrying in ${delay}s"
      sleep "${delay}"
    fi
  done

  fail "failed to create manifest ${target_ref}"
}

cmd_version() {
  read_version
}

cmd_build() {
  local image_repo
  local version
  local arch
  local image_ref

  require_cmd docker

  image_repo="$(resolve_image_repo)"
  version="$(read_version)"
  arch="$(detect_local_arch)"
  image_ref="${image_repo}:${version}"

  log "Using env file ${ENV_FILE}"
  log "Using version ${version} from ${VERSION_FILE}"
  run_buildx_image "${arch}" "${image_ref}" load "${version}"
  docker tag "${image_ref}" "${image_repo}:local"
  log "Built ${image_ref} and ${image_repo}:local"
}

cmd_run() {
  local image_repo
  local docker_port

  require_cmd docker

  image_repo="$(resolve_image_repo)"
  if [ -n "${CIDR_API_DOCKER_PORT:-}" ]; then
    docker_port="${CIDR_API_DOCKER_PORT}"
  elif [ -n "${DOCKER_PORT:-}" ]; then
    docker_port="${DOCKER_PORT}"
  else
    docker_port="$(read_env_value "CIDR_API_DOCKER_PORT" "30662")"
  fi

  log "Running ${image_repo}:local on http://127.0.0.1:${docker_port}"
  docker run --rm -p "${docker_port}:30662" "${image_repo}:local"
}

cmd_publish() {
  local image_repo
  local version
  local platforms
  local platform
  local arch
  local image_ref
  local version_ref
  local latest_ref
  local arch_refs=()

  require_cmd docker

  image_repo="$(resolve_image_repo)"
  version="$(read_version)"
  platforms="$(resolve_platforms)"
  version_ref="${image_repo}:${version}"
  latest_ref="${image_repo}:latest"

  log "Using env file ${ENV_FILE}"
  log "Publishing ${version_ref}"
  log "latest will be updated to the same manifest"

  IFS=',' read -r -a platform_list <<< "${platforms}"
  for platform in "${platform_list[@]}"; do
    arch="$(platform_to_arch "${platform}")"
    image_ref="${image_repo}:${version}-${arch}"
    arch_refs+=("${image_ref}")
    run_buildx_image "${arch}" "${image_ref}" push "${version}"
  done

  create_manifest_tag "${version_ref}" "${arch_refs[@]}"
  create_manifest_tag "${latest_ref}" "${arch_refs[@]}"

  log "Published ${version_ref} and ${latest_ref}"
}

usage() {
  cat <<'EOF'
Usage:
  bash ./scripts/docker-release.sh <command>

Commands:
  version   Print the Docker image version from VERSION
  build     Build and load the local-architecture Docker image
  run       Run the local image on host port 30662 by default
  publish   Push amd64/arm64 images, create the version manifest, and update latest

Config:
  VERSION                                 fixed release version file
  deploy/docker/.env                     optional local overrides
  CIDR_API_DOCKER_IMAGE_REPO             Docker Hub namespace/repo
  CIDR_API_DOCKER_PLATFORMS              default: linux/amd64,linux/arm64
  CIDR_API_DOCKER_PORT                   default: 30662
  CIDR_API_DOCKER_CACHE_DIR              default: $HOME/.cache/go-cidr-api-buildx
  CIDR_API_DOCKER_BUILDER                optional existing buildx builder
  CIDR_API_DOCKER_MANAGED_BUILDER        default: go-cidr-api-buildx
  CIDR_API_DOCKER_HTTP_PROXY             optional build proxy; falls back to HTTP_PROXY/http_proxy
  CIDR_API_DOCKER_HTTPS_PROXY            optional build proxy; falls back to HTTPS_PROXY/https_proxy
  CIDR_API_DOCKER_ALL_PROXY              optional build proxy; falls back to ALL_PROXY/all_proxy
  CIDR_API_DOCKER_NO_PROXY               optional no_proxy; falls back to NO_PROXY/no_proxy
  CIDR_API_DOCKER_PROXY_HOST_ALIAS       default: host.docker.internal
  CIDR_API_DOCKER_MANIFEST_RETRY_ATTEMPTS default: 5
  CIDR_API_DOCKER_MANIFEST_RETRY_DELAY   default: 5 seconds

Taskfile aliases:
  DOCKER_IMAGE_REPO                      forwarded to CIDR_API_DOCKER_IMAGE_REPO
  DOCKER_PLATFORMS                       forwarded to CIDR_API_DOCKER_PLATFORMS
  DOCKER_PORT                            forwarded to CIDR_API_DOCKER_PORT
EOF
}

cd "${ROOT_DIR}"

case "${1:-}" in
  version)
    cmd_version
    ;;
  build)
    cmd_build
    ;;
  run)
    cmd_run
    ;;
  publish)
    cmd_publish
    ;;
  *)
    usage
    exit 1
    ;;
esac
