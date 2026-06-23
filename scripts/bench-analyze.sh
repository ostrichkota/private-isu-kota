#!/bin/bash
set -euo pipefail

ALP="${HOME}/go/bin/alp"
NGINX_LOG="/var/log/nginx/access.log"
SLOW_LOG="/var/lib/mysql/ip-192-168-1-10-slow.log"
BENCH_DIR="$(cd "$(dirname "$0")/../benchmarker" && pwd)"

echo "== 1. ログクリア =="
sudo truncate -s 0 "$NGINX_LOG"
sudo sh -c "> $SLOW_LOG"

echo "== 2. ベンチマーク実行 =="
cd "$BENCH_DIR"
./bin/benchmarker -t http://localhost -u ./userdata

echo ""
echo "== 3. alp: 遅いパス Top（合計時間順） =="
# head が先に終了すると alp が SIGPIPE(141) になり pipefail でスクリプトが止まるため || true
sudo "$ALP" ltsv \
  --file "$NGINX_LOG" \
  --uri-label path \
  --sort sum --reverse \
  -o count,sum,avg,max,method,uri \
  -m "GET /,GET /posts,POST /login,POST /register,GET /image,POST /comment,POST /,GET /@" \
  | head -15 || true

echo ""
echo "== 4. slow query log: 重いクエリ Top =="
slow_output="$(sudo mysqldumpslow -s t -t 10 "$SLOW_LOG" 2>/dev/null || true)"
if [[ -n "$slow_output" ]]; then
  echo "$slow_output"
else
  echo "(slow log なし)"
fi
