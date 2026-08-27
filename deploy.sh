#!/usr/bin/env bash

set -Eeuo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
image_tag="cqingwang/litellm:latest"

usage() {
  printf '%s\n' "用法: $0 --build | --dev"
}

require_docker() {
  if ! command -v docker >/dev/null 2>&1; then
    printf '%s\n' '错误: 未找到 docker 命令' >&2
    exit 1
  fi
  if ! docker info >/dev/null 2>&1; then
    printf '%s\n' '错误: Docker daemon 不可用' >&2
    exit 1
  fi
}

build_image() {
  require_docker
  docker build --platform=linux/amd64 --file "$script_dir/Dockerfile" --tag "$image_tag" "$script_dir"
  image_id="$(docker image inspect --format '{{.Id}}' "$image_tag")"
  printf '镜像构建完成: %s (%s)\n' "$image_tag" "$image_id"
}

needs_rebuild() {
  local target="$1"
  shift
  if [[ ! -f "$target" ]]; then
    return 0
  fi
  for source_path in "$@"; do
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
    \( -path "$script_dir/.git" -o -path "$script_dir/.dev" -o -path "$script_dir/web/node_modules" \) -prune \
    -o -type f \( -name '*.go' -o -name 'go.mod' -o -name 'go.sum' -o -name 'VERSION' \) -newer "$target" -print -quit)" ]]
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf '错误: 未找到 %s 命令\n' "$1" >&2
    exit 1
  fi
}

build_local_web() {
  require_command bun
  local web_index="$script_dir/web/dist/index.html"
  if needs_rebuild "$web_index" "$script_dir/web/src" "$script_dir/web/package.json" "$script_dir/web/bun.lock"; then
    printf '%s\n' '正在使用本地 Bun 构建前端产物...'
    (cd "$script_dir/web" && bun run build)
  else
    printf '%s\n' '前端产物未变化，复用 web/dist。'
  fi
}

build_local_binary() {
  require_command go
  local dev_dir="$script_dir/.dev"
  local dev_binary="$dev_dir/new-api"
  mkdir -p "$dev_dir"
  if needs_binary_rebuild "$dev_binary" || needs_rebuild "$dev_binary" "$script_dir/web/dist"; then
    printf '%s\n' '正在使用本地 Go 构建 Linux/amd64 运行文件...'
    (
      cd "$script_dir"
      GOOS=linux GOARCH=amd64 CGO_ENABLED=0 GOWORK=off \
        go build -ldflags "-s -w -X 'github.com/QuantumNous/new-api/common.Version=$(<VERSION)'" \
        -o "$dev_binary"
    )
  else
    printf '%s\n' '本地运行文件未变化，复用 .dev/new-api。'
  fi
  printf '%s\n' "$dev_binary"
}

run_dev() {
  require_docker
  if ! docker image inspect "$image_tag" >/dev/null 2>&1; then
    printf '错误: 本地不存在 %s，请先执行 ./deploy.sh --build\n' "$image_tag" >&2
    exit 1
  fi
  container_name="${LITELLM_DEV_CONTAINER:-litellm-dev}"
  if docker ps -a --format '{{.Names}}' | grep -Fxq "$container_name"; then
    printf '调试容器 %s 已存在，删除后重新启动。\n' "$container_name"
    docker rm -f "$container_name" >/dev/null
  fi
  build_local_web >/dev/null
  dev_binary="$(build_local_binary | tail -1)"
  if [[ ! -x "$dev_binary" ]]; then
    printf '错误: 本地运行文件不可执行: %s\n' "$dev_binary" >&2
    exit 1
  fi
  port="${LITELLM_DEV_PORT:-3000}"
  docker run --rm --name "$container_name" --platform linux/amd64 \
    --publish "$port:3000" \
    --volume "$dev_binary:/new-api:ro" \
    --volume "$script_dir/data:/data" \
    "$image_tag"
}

if [[ $# -ne 1 ]]; then
  usage >&2
  exit 2
fi

case "$1" in
  --build) build_image ;;
  --dev) run_dev ;;
  *)
    usage >&2
    exit 2
    ;;
esac
