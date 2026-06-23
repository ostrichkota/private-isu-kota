# ISUCON 研修メモ（Go 切り替え + インデックス）

## 実施内容

1. アプリ言語を Ruby → Go に切り替え（`isu-ruby` 停止、`isu-go` 起動）
2. MySQL にインデックス 3 本を追加（`sql/add_indexes.sql`）

## ベンチマーク結果

| タイミング | pass | score | success | fail | 備考 |
|-----------|------|-------|---------|------|------|
| Go 素の状態 | true | 0 | 495 | 56 | タイムアウト多発 |
| インデックス追加後 | true | 15139 | 14253 | 0 | fail 解消 |

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

## 次にやること

- [ ] openssl → ネイティブ SHA512（login/register）
- [ ] `GET /@xxx` のクエリ改善
- [ ] `SetMaxOpenConns` 等の Go チューニング
