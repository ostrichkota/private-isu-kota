#!/bin/bash
# 画像ファイルから DB の imgdata を復元する（1件ずつ処理で OOM 回避）
set -euo pipefail

cd "$(dirname "$0")/../webapp/golang"
go build -o /tmp/restore-images ./cmd/restore-images
/tmp/restore-images
