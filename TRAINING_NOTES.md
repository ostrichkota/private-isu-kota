# ISUCON 研修メモ（Go 切り替え + インデックス）

## 進捗

- [x] アプリ言語を Ruby → Go に切り替え（`isu-ruby` 停止、`isu-go` 起動）
- [x] MySQL にインデックス 3 本を追加（`sql/add_indexes.sql`）
- [x] 計測環境セットアップ（slow log / alp / nginx LTSV / `scripts/bench-analyze.sh`）
- [x] ベンチ → ログ解析でボトルネック Top 3 をメモ（alp・slow log）
- [x] `makePosts` の N+1 をバルク取得で解消（`webapp/golang/app.go`）
- [x] openssl → ネイティブ SHA512（login/register）
- [x] `GET /@xxx` のクエリ改善
- [x] `SetMaxOpenConns` 等の Go チューニング
- [x] nginx gzip 有効化（CSS/JS 含む gzip_types + gzip_proxied）

## ベンチマーク結果

| タイミング | pass | score | success | fail | 備考 |
|-----------|------|-------|---------|------|------|
| Go 素の状態 | true | 0 | 495 | 56 | タイムアウト多発 |
| インデックス追加後 | true | 15139 | 14253 | 0 | fail 解消 |
| N+1 解消 + SHA512 ネイティブ化後 | true | 36027 | 32272 | 0 | pass 維持 |
| DB 接続プール調整後 | true | 45524 | 40676 | 0 | MaxOpen/Idle=80 |
| nginx gzip 有効化後 | true | 46899 | 41762 | 0 | CSS/JS も圧縮 |

```bash
cd benchmarker
./bin/benchmarker -t http://localhost -u ./userdata
```

## 計測環境

### セットアップ

```bash
# MySQL slow query log（scripts/mysql-slow-log.cnf.example を参照）
sudo mysql -e "SET GLOBAL slow_query_log = 1; SET GLOBAL long_query_time = 0.1;"

# alp インストール
go install github.com/tkuchiki/alp/cmd/alp@latest

# nginx LTSV ログ（scripts/nginx-ltsv.conf.example を参照）
sudo nginx -t && sudo systemctl reload nginx

# nginx gzip（scripts/nginx-gzip.conf.example を参照）
sudo cp scripts/nginx-gzip.conf.example /etc/nginx/conf.d/isucon-gzip.conf
sudo nginx -t && sudo systemctl reload nginx
```

### 解析（一括）

```bash
bash scripts/bench-analyze.sh
```

### 遅いパス Top 3（alp・合計時間）

| 順位 | パス | SUM（秒） |
|------|------|-----------|
| 1 | GET / | 57.0 |
| 2 | POST /login | 53.0 |
| 3 | POST /register | 16.8 |

### 重いクエリ Top 3（slow log）

| 順位 | クエリ |
|------|--------|
| 1 | `SELECT COUNT(*) FROM comments WHERE user_id = ?` |
| 2 | `INSERT INTO posts (...)` |
| 3 | `DELETE FROM posts WHERE id > ?`（initialize） |
