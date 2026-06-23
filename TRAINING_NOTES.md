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
- [x] 画像のファイル退避 + nginx 直接配信
- [x] テンプレートの事前パース（起動時1回のみ ParseFiles）
- [x] コメント最新3件の SQL 化（`ROW_NUMBER()` で DB 側で絞り込み）
- [x] `GET /` 向け: コメント数+コメント取得の1クエリ統合、セッションユーザーの memcached キャッシュ
- [x] `GET /posts/:id` の `SELECT *` 見直し（imgdata 除外）
- [x] 表示用ユーザー取得で passhash を読まない（makePosts / プロフィール / 管理画面）
- [x] getCSRFToken の二重呼び出し解消、BAN 時のユーザキャッシュ削除
- [x] nginx で静的ファイルを try_files 直接配信
- [x] GET /・GET /posts で DB 取得とセッション参照を並列化

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
| 画像ファイル退避後 | true | 68182 | 60932 | 0 | nginx 直接配信 |
| imgdata 復元後（二重管理維持） | true | 65226 | 58321 | 0 | |
| テンプレ事前パース + コメント SQL 化後 | true | 71941 | 65019 | 0 | |
| GET / 最適化（コメント統合+ユーザキャッシュ）後 | true | 72321 | 65234 | 0 | |
| アプリクエリ最適化一式後 | true | 72390 | 65469 | 0 | training/optimize-get-index |
| nginx 静的ファイル直接配信後 | true | 77662 | 70342 | 0 | try_files |
| GET/GET /posts 並列化後 | true | 77833 | 70446 | 0 | |

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

# nginx 画像直接配信（scripts/nginx-image-location.conf.example を isucon.conf に追加）
sudo nginx -t && sudo systemctl reload nginx

# nginx 静的ファイル直接配信（scripts/nginx-static-files.conf.example を isucon.conf に反映）
sudo nginx -t && sudo systemctl reload nginx

# 既存画像のエクスポート（初回のみ・OOM 回避のためビルド済みバイナリ使用）
bash scripts/export-post-images.sh
```

## 次にやること

- [ ] `GET /posts/:id` の深掘り（imgdata 除外済み・まだ ~20s）
- [ ] `GET /image/:id.:ext` に `Cache-Control`（nginx 側）
- [ ] PR 作成（training/optimize-get-index）
- [ ] 振り返り最終記入
