-- GET /@xxx 改善: comments.user_id の COUNT 用
-- 適用: mysql -u isuconp -pisuconp isuconp < sql/add_index_comments_user_id.sql

CREATE INDEX idx_comments_user_id ON comments(user_id);
