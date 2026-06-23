# ISUCON 研修メモ（Go 切り替え + インデックス）

## 進捗

- [x] アプリ言語を Ruby → Go に切り替え（`isu-ruby` 停止、`isu-go` 起動）
- [x] MySQL にインデックス 3 本を追加（`sql/add_indexes.sql`）
- [x] MySQL `comments(user_id)` インデックス追加（`sql/add_index_comments_user_id.sql`）
- [x] 計測環境セットアップ（slow log / alp / nginx LTSV / `scripts/bench-analyze.sh`）
- [x] ベンチ → ログ解析でボトルネック Top 3 をメモ（alp・slow log）
- [x] `makePosts` の N+1 をバルク取得で解消（`webapp/golang/app.go`）
- [x] openssl → ネイティブ SHA512（login/register）
- [x] `GET /@xxx` のクエリ改善
- [x] `SetMaxOpenConns` 等の Go チューニング（MaxOpen/Idle=80, interpolateParams）
- [x] MySQL InnoDB チューニング（buffer pool 768M / flush_log_at_trx_commit=2 / binlog 1日）

## ベンチマーク結果

| タイミング | pass | score | success | fail | 備考 |
|-----------|------|-------|---------|------|------|
| Go 素の状態 | true | 0 | 495 | 56 | タイムアウト多発 |
| インデックス追加後 | true | 15139 | 14253 | 0 | fail 解消 |
| N+1 解消 + SHA512 ネイティブ化後 | true | 36027 | 32272 | 0 | pass 維持 |
| `GET /@xxx` クエリ改善後 | true | 39342 | 35158 | 0 | pass 維持 |
| MySQL InnoDB チューニング後 | true | 41214 | 36804 | 0 | buffer pool 1G, flush=2 |
| buffer pool 768M に調整後 | true | 46121 | 41010 | 0 | メモリ 3.7GB 向け |
| DB チューニング一式後 | true | 47284 | 42152 | 0 | pool+InnoDB+binlog |

buffer pool 比較（同一環境・flush=2）:

| buffer pool | score（代表値） |
|-------------|----------------|
| 1G | 46475 |
| 768M | 46121 |
| 512M | 45880 |

```bash
cd benchmarker
./bin/benchmarker -t http://localhost -u ./userdata
bash scripts/bench-analyze.sh
```

MySQL チューニング反映:

```bash
sudo cp scripts/mysql-tuning.cnf.example /etc/mysql/conf.d/isucon-tuning.cnf
sudo systemctl restart mysql
```

## 計測環境

### セットアップ

```bash
# MySQL slow query log
sudo cp scripts/mysql-slow-log.cnf.example /etc/mysql/conf.d/isucon-slow-log.cnf

# MySQL InnoDB チューニング
sudo cp scripts/mysql-tuning.cnf.example /etc/mysql/conf.d/isucon-tuning.cnf
sudo systemctl restart mysql

# alp インストール
go install github.com/tkuchiki/alp/cmd/alp@latest

# nginx LTSV + gzip
sudo cp scripts/nginx-gzip.conf.example /etc/nginx/conf.d/isucon-gzip.conf
sudo nginx -t && sudo systemctl reload nginx
```

## 次にやること

- [ ] `GET /` / `GET /posts` のクエリ改善
- [ ] 画像のファイル退避（`INSERT posts` の imgdata 対策）
- [ ] `GET /image/:id.:ext` に `Cache-Control` / `ETag`
- [ ] `/posts/:id` の N+1
- [ ] 振り返り最終記入
