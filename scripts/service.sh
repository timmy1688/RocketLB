#!/usr/bin/env bash
# 启停开发环境里的控制面。数据库仍由 docker compose 的 postgres 服务提供。
# 生产环境不要用这个脚本，直接 docker compose up -d。
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

pidfile="$root/data/run/rocketlb.pid"
logfile="$root/data/run/rocketlb.log"
bin="$root/bin/rocketlb"
addr="${ROCKETLB_HTTP_ADDR:-:8080}"
port="${addr##*:}"

usage() {
  echo "用法: $0 {start|stop|restart|status}" >&2
  exit 2
}

postgres_up() {
  docker compose up -d --wait postgres
}

pid_running() {
  local pid="${1:-}"
  [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null
}

current_pid() {
  [[ -f "$pidfile" ]] || return 1
  local pid
  pid=$(tr -d '[:space:]' <"$pidfile")
  pid_running "$pid" || return 1
  printf '%s\n' "$pid"
}

wait_healthy() {
  local pid=$1 i
  for i in $(seq 1 40); do
    if curl -fsS --noproxy '*' "http://127.0.0.1:${port}/healthz" >/dev/null 2>&1; then
      return 0
    fi
    if ! pid_running "$pid"; then
      return 1
    fi
    sleep 0.25
  done
  return 1
}

start() {
  local pid
  if pid=$(current_pid); then
    echo "开发环境已在运行 pid=$pid"
    exit 0
  fi
  if ss -ltn | awk '{print $4}' | grep -q ":${port}$"; then
    echo "端口 ${port} 已被占用。若生产容器在跑，先执行 docker compose stop rocketlb" >&2
    exit 1
  fi
  mkdir -p "$root/data/run" "$root/bin"
  postgres_up
  echo "编译控制面"
  GOTOOLCHAIN=local go build -o "$bin" ./cmd/rocketlb
  if [[ -f "$root/configs/dev.env" ]]; then
    set -a
    # shellcheck disable=SC1091
    source "$root/configs/dev.env"
    set +a
  fi
  export ROCKETLB_ENV="${ROCKETLB_ENV:-debug}"
  nohup "$bin" >>"$logfile" 2>&1 &
  pid=$!
  echo "$pid" >"$pidfile"
  if wait_healthy "$pid"; then
    echo "已启动 pid=$pid  http://127.0.0.1:${port}"
    echo "日志 $logfile"
    return 0
  fi
  echo "启动失败，最后日志：" >&2
  tail -n 40 "$logfile" >&2 || true
  exit 1
}

stop() {
  local pid
  if ! pid=$(current_pid); then
    rm -f "$pidfile"
    echo "开发环境未在运行"
    return 0
  fi
  kill -TERM "$pid" 2>/dev/null || true
  local i
  for i in $(seq 1 25); do
    pid_running "$pid" || break
    sleep 0.2
  done
  if pid_running "$pid"; then
    kill -KILL "$pid" 2>/dev/null || true
  fi
  rm -f "$pidfile"
  echo "已停止"
}

status() {
  local pid
  if pid=$(current_pid); then
    echo "运行中 pid=$pid"
    curl -fsS --noproxy '*' "http://127.0.0.1:${port}/healthz" || true
    echo
    return 0
  fi
  echo "未运行"
  return 1
}

case "${1:-}" in
  start) start ;;
  stop) stop ;;
  restart) stop; start ;;
  status) status ;;
  *) usage ;;
esac
