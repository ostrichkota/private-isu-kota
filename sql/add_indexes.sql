-- ISUCON 研修: ベンチマーク改善用インデックス
-- 適用: mysql -u isuconp -pisuconp isuconp < webapp/sql/add_indexes.sql

CREATE INDEX idx_posts_created_at ON posts(created_at);
CREATE INDEX idx_posts_user_created ON posts(user_id, created_at);
CREATE INDEX idx_comments_post_created ON comments(post_id, created_at);
