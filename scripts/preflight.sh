#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ROOT_DIR}/deploy/.env"
if [[ ! -f "$ENV_FILE" ]]; then
  echo "缺少 deploy/.env；请先 cp deploy/.env.example deploy/.env 并配置 NAS_HOME_PUBLIC_HOST" >&2
  exit 1
fi
set -a
source "$ENV_FILE"
set +a

port="${NAS_HOME_PORT:-1111}"
host="${NAS_HOME_PUBLIC_HOST:-}"
if [[ -z "$host" || "$host" == "localhost" || "$host" == "127.0.0.1" ]]; then
  echo "NAS_HOME_PUBLIC_HOST 必须是 NAS 可被客户端访问的主机名或 IP（本机模式除外）" >&2
  exit 1
fi
if ! docker info >/dev/null 2>&1; then
  echo "Docker daemon 不可用" >&2
  exit 1
fi
if [[ ! -S /var/run/docker.sock ]]; then
  echo "未找到 /var/run/docker.sock；若使用 socket proxy，请确认代理地址和 compose 配置" >&2
  exit 1
fi
if (command -v ss >/dev/null && ss -ltn "sport = :${port}" | tail -n +2 | grep -q .) || (command -v nc >/dev/null && nc -z 127.0.0.1 "$port" >/dev/null 2>&1); then
  echo "端口 ${port} 已被占用" >&2
  exit 1
fi
data_dir="${ROOT_DIR}/data"
mkdir -p "$data_dir"
[[ -w "$data_dir" ]] || { echo "数据目录不可写: $data_dir" >&2; exit 1; }
cd "$ROOT_DIR"
docker compose --env-file "$ENV_FILE" -f deploy/compose.yml config --quiet
echo "preflight passed: port=${port}, public_host=${host}, docker=available"
