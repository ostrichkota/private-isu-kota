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

## 次にやること

- [ ] `makePosts` の N+1 解消（`webapp/golang/app.go`）
- [ ] slow query log / alp でボトルネック確認
- [ ] `SetMaxOpenConns` 等の Go チューニング
