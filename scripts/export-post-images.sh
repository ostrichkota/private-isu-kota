#!/bin/bash
# 既存投稿の imgdata をファイルに書き出す（初回のみ・1件ずつ処理で OOM 回避）
set -euo pipefail

cd "$(dirname "$0")/../webapp/golang"
go build -o /tmp/export-images ./cmd/export-images
/tmp/export-images
