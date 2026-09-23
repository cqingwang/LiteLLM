#!/usr/bin/env bash

set -Eeuo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
image_tag="litellm:latest"
platform="linux/amd64"

usage() {
  printf '%s\n' "用法: $0 --build | --dev"
  printf '%s\n' '配置:'
  printf '%s\n' '  镜像标签                litellm:latest'
  printf '%s\n' '  目标平台                linux/amd64'
  printf '%s\n' '  AXONHUB_DEV_CONTAINER   开发容器名，默认 axonhub-dev'
  printf '%s\n' '  AXONHUB_DEV_PORT        宿主机端口，默认 8090'
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf '错误: 未找到 %s 命令\n' "$1" >&2
    exit 1
  fi
}

require_docker() {
  require_command docker
  if ! docker info >/dev/null 2>&1; then
    printf '%s\n' '错误: Docker daemon 不可用' >&2
    exit 1
  fi
}

needs_rebuild() {
  local target="$1"
  shift

  if [[ ! -f "$target" ]]; then
    return 0
  fi

  for source_path in "$@"; do
    if [[ -f "$source_path" && "$source_path" -nt "$target" ]]; then
      return 0
    fi
    if [[ -d "$source_path" ]] && [[ -n "$(find "$source_path" -type f -newer "$target" -print -quit)" ]]; then
      return 0
    fi
  done

  return 1
}

needs_binary_rebuild() {
  local target="$1"

  if [[ ! -f "$target" ]]; then
    return 0
  fi

  [[ -n "$(find "$script_dir" \
    \( -path "$script_dir/.git" -o -path "$script_dir/.dev" -o -path "$script_dir/frontend/node_modules" \) -prune \
    -o -type f \( -name '*.go' -o -name 'go.mod' -o -name 'go.sum' -o -name 'VERSION' \) -newer "$target" -print -quit)" ]]
}

build_image() {
  prepare_local_artifacts
  require_docker
  docker build --platform "$platform" --file "$script_dir/Dockerfile" --tag "$image_tag" "$script_dir"
  local image_id
  image_id="$(docker image inspect --format '{{.Id}}' "$image_tag")"
  printf '镜像构建完成: %s (%s)\n' "$image_tag" "$image_id"
}

build_local_frontend() {
  local frontend_dist="$script_dir/frontend/dist/index.html"
  local vite_binary="$script_dir/frontend/node_modules/.bin/vite"
  if [[ ! -x "$vite_binary" ]]; then
    printf '%s\n' '错误: 未找到 frontend/node_modules/.bin/vite，请先在 frontend 目录执行 pnpm install' >&2
    exit 1
  fi

  if needs_rebuild "$frontend_dist" "$script_dir/frontend/src" "$script_dir/frontend/package.json" "$script_dir/frontend/pnpm-lock.yaml"; then
    printf '%s\n' '正在使用本地 Vite 构建前端产物...'
    (cd "$script_dir/frontend" && "$vite_binary" build)
  else
    printf '%s\n' '前端产物未变化，复用 frontend/dist。'
  fi

  printf '%s\n' '正在同步前端静态资源到 Go embed 目录...'
  mkdir -p "$script_dir/internal/server/static/dist"
  find "$script_dir/internal/server/static/dist" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
  cp -R "$script_dir/frontend/dist/." "$script_dir/internal/server/static/dist/"
}

build_local_binary() {
  require_command go

  local dev_dir="$script_dir/.dev"
  local dev_binary="$dev_dir/axonhub"
  local go_arch='amd64'

  mkdir -p "$dev_dir"
  if needs_binary_rebuild "$dev_binary" || needs_rebuild "$dev_binary" "$script_dir/internal/server/static/dist"; then
    printf '正在使用本地 Go 构建 %s 运行文件...\n' "$platform"
    (
      cd "$script_dir"
      GOOS=linux GOARCH="$go_arch" CGO_ENABLED=0 GOWORK=off GOTOOLCHAIN=auto \
        go build -tags=nomsgpack \
        -ldflags "-s -w -X 'github.com/looplj/axonhub/internal/build.Version=$(cat internal/build/VERSION 2>/dev/null || echo dev)'" \
        -o "$dev_binary" \
        ./cmd/axonhub
    )
  else
    printf '%s\n' '本地运行文件未变化，复用 .dev/axonhub。'
  fi

  printf '%s\n' "$dev_binary"
}

prepare_ca_certificates() {
  local certificate_source=""
  local certificate_target="$script_dir/.dev/ca-certificates.crt"

  if [[ -f /etc/ssl/certs/ca-certificates.crt ]]; then
    certificate_source='/etc/ssl/certs/ca-certificates.crt'
  elif [[ -f /etc/ssl/cert.pem ]]; then
    certificate_source='/etc/ssl/cert.pem'
  else
    printf '%s\n' '错误: 未找到系统 CA 证书，请安装 CA 证书后重试' >&2
    exit 1
  fi

  cp "$certificate_source" "$certificate_target"
  mkdir -p "$script_dir/.dev/tmp"
  : > "$script_dir/.dev/tmp/.keep"
}

prepare_local_artifacts() {
  rm -rf ./.dev
  build_local_frontend
  build_local_binary >/dev/null
  prepare_ca_certificates
}

run_dev() {
  require_docker
  if ! docker image inspect "$image_tag" >/dev/null 2>&1; then
    printf '错误: 本地不存在 %s，请先执行 ./deploy.sh --build\n' "$image_tag" >&2
    exit 1
  fi

  local container_name="${AXONHUB_DEV_CONTAINER:-axonhub-dev}"
  local port="${AXONHUB_DEV_PORT:-8090}"
  local dev_binary

  if docker ps -a --format '{{.Names}}' | grep -Fxq "$container_name"; then
    printf '开发容器 %s 已存在，删除后重新启动。\n' "$container_name"
    docker rm -f "$container_name" >/dev/null
  fi

  build_local_frontend
  dev_binary="$(build_local_binary | tail -n1)"
  if [[ ! -x "$dev_binary" ]]; then
    printf '错误: 本地运行文件不可执行: %s\n' "$dev_binary" >&2
    exit 1
  fi

  mkdir -p "$script_dir/data"
  local docker_args=(
    run --rm --name "$container_name" --platform "$platform"
    --publish "$port:8090"
    --volume "$dev_binary:/app/axonhub:ro"
    --volume "$script_dir/data:/data"
    --env "AXONHUB_DB_DIALECT=${AXONHUB_DB_DIALECT:-sqlite3}"
    --env "AXONHUB_DB_DSN=${AXONHUB_DB_DSN:-file:/data/axonhub.db?cache=shared&_fk=1&_pragma=journal_mode(WAL)}"
  )

  if [[ -f "$script_dir/config.yml" ]]; then
    docker_args+=(--volume "$script_dir/config.yml:/app/config.yml:ro")
  fi

  docker_args+=("$image_tag")
  printf '开发服务启动: http://localhost:%s\n' "$port"
  docker "${docker_args[@]}"
}

if [[ $# -ne 1 ]]; then
  usage >&2
  exit 2
fi

case "$1" in
  --build) build_image ;;
  --dev) run_dev ;;
  --help|-h) usage ;;
  *)
    usage >&2
    exit 2
    ;;
esac
