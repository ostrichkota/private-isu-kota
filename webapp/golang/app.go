package main

import (
	"context"
	crand "crypto/rand"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	gsm "github.com/bradleypeabody/gorilla-sessions-memcache"
	"github.com/go-chi/chi/v5"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/gorilla/sessions"
	"github.com/jmoiron/sqlx"
)

var (
	db    *sqlx.DB
	store *gsm.MemcacheStore

	tmplLogin    *template.Template
	tmplRegister *template.Template
	tmplIndex    *template.Template
	tmplUser     *template.Template
	tmplPosts    *template.Template
	tmplPostID   *template.Template
	tmplBanned   *template.Template
)

const (
	postsPerPage     = 20
	ISO8601Format    = "2006-01-02T15:04:05-07:00"
	UploadLimit      = 10 * 1024 * 1024 // 10mb
	imageDir         = "/home/isucon/private_isu/webapp/images"
	userCacheSeconds = 60
)

type User struct {
	ID          int       `db:"id"`
	AccountName string    `db:"account_name"`
	Passhash    string    `db:"passhash"`
	Authority   int       `db:"authority"`
	DelFlg      int       `db:"del_flg"`
	CreatedAt   time.Time `db:"created_at"`
}

type Post struct {
	ID           int       `db:"id"`
	UserID       int       `db:"user_id"`
	Imgdata      []byte    `db:"imgdata"`
	Body         string    `db:"body"`
	Mime         string    `db:"mime"`
	CreatedAt    time.Time `db:"created_at"`
	CommentCount int
	Comments     []Comment
	User         User
	CSRFToken    string
}

type Comment struct {
	ID        int       `db:"id"`
	PostID    int       `db:"post_id"`
	UserID    int       `db:"user_id"`
	Comment   string    `db:"comment"`
	CreatedAt time.Time `db:"created_at"`
	User      User
}

var memcacheClient *memcache.Client

func init() {
	memdAddr := os.Getenv("ISUCONP_MEMCACHED_ADDRESS")
	if memdAddr == "" {
		memdAddr = "localhost:11211"
	}
	memcacheClient = memcache.New(memdAddr)
	store = gsm.NewMemcacheStore(memcacheClient, "iscogram_", []byte("sendagaya"))
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
}

func dbInitialize(ctx context.Context) {
	sqls := []string{
		"DELETE FROM users WHERE id > 1000",
		"DELETE FROM posts WHERE id > 10000",
		"DELETE FROM comments WHERE id > 100000",
		"UPDATE users SET del_flg = 0",
		"UPDATE users SET del_flg = 1 WHERE id % 50 = 0",
	}

	for _, sql := range sqls {
		db.ExecContext(ctx, sql)
	}
	deletePostImagesAbove(10000)
}

func extFromMime(mime string) string {
	switch mime {
	case "image/jpeg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	default:
		return ""
	}
}

func mimeFromExt(ext string) string {
	switch ext {
	case "jpg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	default:
		return ""
	}
}

func imageFilePath(id int, ext string) string {
	return path.Join(imageDir, strconv.Itoa(id)+"."+ext)
}

func savePostImage(id int, mime string, data []byte) error {
	ext := extFromMime(mime)
	if ext == "" {
		return fmt.Errorf("unknown mime: %s", mime)
	}
	return os.WriteFile(imageFilePath(id, ext), data, 0644)
}

func deletePostImagesAbove(id int) {
	entries, err := os.ReadDir(imageDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		dot := strings.LastIndex(name, ".")
		if dot <= 0 {
			continue
		}
		pid, err := strconv.Atoi(name[:dot])
		if err != nil || pid <= id {
			continue
		}
		os.Remove(path.Join(imageDir, name))
	}
}

func tryLogin(ctx context.Context, accountName, password string) *User {
	u := User{}
	err := db.GetContext(ctx, &u, "SELECT * FROM users WHERE account_name = ? AND del_flg = 0", accountName)
	if err != nil {
		return nil
	}

	if calculatePasshash(ctx, u.AccountName, password) == u.Passhash {
		return &u
	} else {
		return nil
	}
}

func validateUser(accountName, password string) bool {
	return regexp.MustCompile(`\A[0-9a-zA-Z_]{3,}\z`).MatchString(accountName) &&
		regexp.MustCompile(`\A[0-9a-zA-Z_]{6,}\z`).MatchString(password)
}

func digest(_ context.Context, src string) string {
	sum := sha512.Sum512([]byte(src))
	return hex.EncodeToString(sum[:])
}

func calculateSalt(ctx context.Context, accountName string) string {
	return digest(ctx, accountName)
}

func calculatePasshash(ctx context.Context, accountName, password string) string {
	return digest(ctx, password+":"+calculateSalt(ctx, accountName))
}

func getSession(r *http.Request) *sessions.Session {
	session, _ := store.Get(r, "isuconp-go.session")

	return session
}

func getSessionUser(r *http.Request) User {
	ctx := r.Context()
	session := getSession(r)
	uid, ok := session.Values["user_id"]
	if !ok || uid == nil {
		return User{}
	}

	var userID int
	switch v := uid.(type) {
	case int:
		userID = v
	case int64:
		userID = int(v)
	default:
		return User{}
	}
	if userID == 0 {
		return User{}
	}

	cacheKey := fmt.Sprintf("u:%d", userID)
	if item, err := memcacheClient.Get(cacheKey); err == nil {
		var u User
		if json.Unmarshal(item.Value, &u) == nil {
			return u
		}
	}

	u := User{}
	err := db.GetContext(ctx, &u, "SELECT `id`, `account_name`, `passhash`, `authority`, `del_flg`, `created_at` FROM `users` WHERE `id` = ?", userID)
	if err != nil {
		return User{}
	}

	if b, err := json.Marshal(u); err == nil {
		_ = memcacheClient.Set(&memcache.Item{
			Key:        cacheKey,
			Value:      b,
			Expiration: userCacheSeconds,
		})
	}

	return u
}

func getFlash(w http.ResponseWriter, r *http.Request, key string) string {
	session := getSession(r)
	value, ok := session.Values[key]

	if !ok || value == nil {
		return ""
	} else {
		delete(session.Values, key)
		session.Save(r, w)
		return value.(string)
	}
}

func makePosts(ctx context.Context, results []Post, csrfToken string, allComments bool) ([]Post, error) {
	if len(results) == 0 {
		return []Post{}, nil
	}

	userIDSet := make(map[int]struct{})
	for _, p := range results {
		userIDSet[p.UserID] = struct{}{}
	}
	userIDs := make([]int, 0, len(userIDSet))
	for id := range userIDSet {
		userIDs = append(userIDs, id)
	}

	users, err := fetchUsersByIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}

	selected := make([]Post, 0, postsPerPage)
	for _, p := range results {
		user, ok := users[p.UserID]
		if !ok || user.DelFlg != 0 {
			continue
		}
		p.User = user
		p.CSRFToken = csrfToken
		selected = append(selected, p)
		if len(selected) >= postsPerPage {
			break
		}
	}

	if len(selected) == 0 {
		return []Post{}, nil
	}

	postIDs := make([]int, len(selected))
	for i, p := range selected {
		postIDs[i] = p.ID
	}

	commentCounts := make(map[int]int, len(postIDs))
	commentsByPost := make(map[int][]Comment)
	if allComments {
		var err error
		commentCounts, err = fetchCommentCountsByPostIDs(ctx, postIDs)
		if err != nil {
			return nil, err
		}
		commentsByPost, err = fetchCommentsByPostIDs(ctx, postIDs, true)
		if err != nil {
			return nil, err
		}
	} else {
		var err error
		commentCounts, commentsByPost, err = fetchCommentsAndCountsByPostIDs(ctx, postIDs)
		if err != nil {
			return nil, err
		}
	}

	commentUserIDSet := make(map[int]struct{})
	for _, comments := range commentsByPost {
		for _, c := range comments {
			if _, ok := users[c.UserID]; !ok {
				commentUserIDSet[c.UserID] = struct{}{}
			}
		}
	}
	if len(commentUserIDSet) > 0 {
		commentUserIDs := make([]int, 0, len(commentUserIDSet))
		for id := range commentUserIDSet {
			commentUserIDs = append(commentUserIDs, id)
		}
		extraUsers, err := fetchUsersByIDs(ctx, commentUserIDs)
		if err != nil {
			return nil, err
		}
		for id, u := range extraUsers {
			users[id] = u
		}
	}

	posts := make([]Post, len(selected))
	for i, p := range selected {
		p.CommentCount = commentCounts[p.ID]
		comments := commentsByPost[p.ID]
		for i, j := 0, len(comments)-1; i < j; i, j = i+1, j-1 {
			comments[i], comments[j] = comments[j], comments[i]
		}
		for j := range comments {
			comments[j].User = users[comments[j].UserID]
		}
		p.Comments = comments
		posts[i] = p
	}

	return posts, nil
}

func fetchUsersByIDs(ctx context.Context, ids []int) (map[int]User, error) {
	if len(ids) == 0 {
		return map[int]User{}, nil
	}
	query, args, err := sqlx.In("SELECT `id`, `account_name`, `authority`, `del_flg`, `created_at` FROM `users` WHERE `id` IN (?)", ids)
	if err != nil {
		return nil, err
	}
	query = db.Rebind(query)

	var users []User
	if err := db.SelectContext(ctx, &users, query, args...); err != nil {
		return nil, err
	}

	m := make(map[int]User, len(users))
	for _, u := range users {
		m[u.ID] = u
	}
	return m, nil
}

type commentWithCount struct {
	Comment
	CommentCount int `db:"comment_count"`
}

func fetchCommentsAndCountsByPostIDs(ctx context.Context, postIDs []int) (map[int]int, map[int][]Comment, error) {
	counts := make(map[int]int, len(postIDs))
	for _, id := range postIDs {
		counts[id] = 0
	}
	if len(postIDs) == 0 {
		return counts, map[int][]Comment{}, nil
	}

	query := `
		SELECT id, post_id, user_id, comment, created_at, comment_count
		FROM (
			SELECT id, post_id, user_id, comment, created_at,
				COUNT(*) OVER (PARTITION BY post_id) AS comment_count,
				ROW_NUMBER() OVER (PARTITION BY post_id ORDER BY created_at DESC) AS rn
			FROM comments
			WHERE post_id IN (?)
		) AS t
		WHERE t.rn <= 3
		ORDER BY post_id, created_at DESC`

	query, args, err := sqlx.In(query, postIDs)
	if err != nil {
		return nil, nil, err
	}
	query = db.Rebind(query)

	var rows []commentWithCount
	if err := db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, nil, err
	}

	commentsByPost := make(map[int][]Comment)
	for _, row := range rows {
		counts[row.PostID] = row.CommentCount
		commentsByPost[row.PostID] = append(commentsByPost[row.PostID], row.Comment)
	}
	return counts, commentsByPost, nil
}

func fetchCommentCountsByPostIDs(ctx context.Context, postIDs []int) (map[int]int, error) {
	m := make(map[int]int, len(postIDs))
	for _, id := range postIDs {
		m[id] = 0
	}
	if len(postIDs) == 0 {
		return m, nil
	}

	type row struct {
		PostID int `db:"post_id"`
		Count  int `db:"count"`
	}
	query, args, err := sqlx.In("SELECT `post_id`, COUNT(*) AS `count` FROM `comments` WHERE `post_id` IN (?) GROUP BY `post_id`", postIDs)
	if err != nil {
		return nil, err
	}
	query = db.Rebind(query)

	var rows []row
	if err := db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	for _, r := range rows {
		m[r.PostID] = r.Count
	}
	return m, nil
}

func getTemplPath(filename string) string {
	return path.Join("templates", filename)
}

func mustParseTemplates() {
	tmplLogin = template.Must(template.ParseFiles(
		getTemplPath("layout.html"),
		getTemplPath("login.html"),
	))
	tmplRegister = template.Must(template.ParseFiles(
		getTemplPath("layout.html"),
		getTemplPath("register.html"),
	))
	tmplBanned = template.Must(template.ParseFiles(
		getTemplPath("layout.html"),
		getTemplPath("banned.html"),
	))

	fmap := template.FuncMap{"imageURL": imageURL}
	tmplIndex = template.Must(template.New("layout.html").Funcs(fmap).ParseFiles(
		getTemplPath("layout.html"),
		getTemplPath("index.html"),
		getTemplPath("posts.html"),
		getTemplPath("post.html"),
	))
	tmplUser = template.Must(template.New("layout.html").Funcs(fmap).ParseFiles(
		getTemplPath("layout.html"),
		getTemplPath("user.html"),
		getTemplPath("posts.html"),
		getTemplPath("post.html"),
	))
	tmplPosts = template.Must(template.New("posts.html").Funcs(fmap).ParseFiles(
		getTemplPath("posts.html"),
		getTemplPath("post.html"),
	))
	tmplPostID = template.Must(template.New("layout.html").Funcs(fmap).ParseFiles(
		getTemplPath("layout.html"),
		getTemplPath("post_id.html"),
		getTemplPath("post.html"),
	))
}

func fetchCommentsByPostIDs(ctx context.Context, postIDs []int, allComments bool) (map[int][]Comment, error) {
	if len(postIDs) == 0 {
		return map[int][]Comment{}, nil
	}

	var query string
	if allComments {
		query = "SELECT `id`, `post_id`, `user_id`, `comment`, `created_at` FROM `comments` WHERE `post_id` IN (?) ORDER BY `post_id`, `created_at` DESC"
	} else {
		query = `
			SELECT id, post_id, user_id, comment, created_at
			FROM (
				SELECT id, post_id, user_id, comment, created_at,
					ROW_NUMBER() OVER (PARTITION BY post_id ORDER BY created_at DESC) AS rn
				FROM comments
				WHERE post_id IN (?)
			) AS t
			WHERE t.rn <= 3
			ORDER BY post_id, created_at DESC`
	}

	query, args, err := sqlx.In(query, postIDs)
	if err != nil {
		return nil, err
	}
	query = db.Rebind(query)

	var all []Comment
	if err := db.SelectContext(ctx, &all, query, args...); err != nil {
		return nil, err
	}

	m := make(map[int][]Comment)
	for _, c := range all {
		m[c.PostID] = append(m[c.PostID], c)
	}
	return m, nil
}

func imageURL(p Post) string {
	ext := ""
	if p.Mime == "image/jpeg" {
		ext = ".jpg"
	} else if p.Mime == "image/png" {
		ext = ".png"
	} else if p.Mime == "image/gif" {
		ext = ".gif"
	}

	return "/image/" + strconv.Itoa(p.ID) + ext
}

func isLogin(u User) bool {
	return u.ID != 0
}

func getCSRFToken(r *http.Request) string {
	session := getSession(r)
	csrfToken, ok := session.Values["csrf_token"]
	if !ok {
		return ""
	}
	return csrfToken.(string)
}

func secureRandomStr(b int) string {
	k := make([]byte, b)
	if _, err := crand.Read(k); err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x", k)
}

func getInitialize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dbInitialize(ctx)
	w.WriteHeader(http.StatusOK)
}

func getLogin(w http.ResponseWriter, r *http.Request) {
	me := getSessionUser(r)

	if isLogin(me) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	tmplLogin.Execute(w, struct {
		Me    User
		Flash string
	}{me, getFlash(w, r, "notice")})
}

func postLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if isLogin(getSessionUser(r)) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	u := tryLogin(ctx, r.FormValue("account_name"), r.FormValue("password"))

	if u != nil {
		session := getSession(r)
		session.Values["user_id"] = u.ID
		session.Values["csrf_token"] = secureRandomStr(16)
		session.Save(r, w)

		http.Redirect(w, r, "/", http.StatusFound)
	} else {
		session := getSession(r)
		session.Values["notice"] = "アカウント名かパスワードが間違っています"
		session.Save(r, w)

		http.Redirect(w, r, "/login", http.StatusFound)
	}
}

func getRegister(w http.ResponseWriter, r *http.Request) {
	if isLogin(getSessionUser(r)) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	tmplRegister.Execute(w, struct {
		Me    User
		Flash string
	}{User{}, getFlash(w, r, "notice")})
}

func postRegister(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if isLogin(getSessionUser(r)) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	accountName, password := r.FormValue("account_name"), r.FormValue("password")

	validated := validateUser(accountName, password)
	if !validated {
		session := getSession(r)
		session.Values["notice"] = "アカウント名は3文字以上、パスワードは6文字以上である必要があります"
		session.Save(r, w)

		http.Redirect(w, r, "/register", http.StatusFound)
		return
	}

	exists := 0
	// ユーザーが存在しない場合はエラーになるのでエラーチェックはしない
	db.GetContext(ctx, &exists, "SELECT 1 FROM users WHERE `account_name` = ?", accountName)

	if exists == 1 {
		session := getSession(r)
		session.Values["notice"] = "アカウント名がすでに使われています"
		session.Save(r, w)

		http.Redirect(w, r, "/register", http.StatusFound)
		return
	}

	query := "INSERT INTO `users` (`account_name`, `passhash`) VALUES (?,?)"
	result, err := db.ExecContext(ctx, query, accountName, calculatePasshash(ctx, accountName, password))
	if err != nil {
		log.Print(err)
		return
	}

	session := getSession(r)
	uid, err := result.LastInsertId()
	if err != nil {
		log.Print(err)
		return
	}
	session.Values["user_id"] = uid
	session.Values["csrf_token"] = secureRandomStr(16)
	session.Save(r, w)

	http.Redirect(w, r, "/", http.StatusFound)
}

func getLogout(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	delete(session.Values, "user_id")
	session.Options = &sessions.Options{MaxAge: -1}
	session.Save(r, w)

	http.Redirect(w, r, "/", http.StatusFound)
}

func getIndex(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := getSessionUser(r)

	results := []Post{}

	err := db.SelectContext(ctx, &results, "SELECT `id`, `user_id`, `body`, `mime`, `created_at` FROM `posts` ORDER BY `created_at` DESC LIMIT ?", postsPerPage*5)
	if err != nil {
		log.Print(err)
		return
	}

	csrfToken := getCSRFToken(r)
	posts, err := makePosts(ctx, results, csrfToken, false)
	if err != nil {
		log.Print(err)
		return
	}

	tmplIndex.Execute(w, struct {
		Posts     []Post
		Me        User
		CSRFToken string
		Flash     string
	}{posts, me, csrfToken, getFlash(w, r, "notice")})
}

func getAccountName(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	accountName := r.PathValue("accountName")
	user := User{}

	err := db.GetContext(ctx, &user, "SELECT `id`, `account_name`, `authority`, `del_flg`, `created_at` FROM `users` WHERE `account_name` = ? AND `del_flg` = 0", accountName)
	if err != nil {
		log.Print(err)
		return
	}

	if user.ID == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	results := []Post{}

	err = db.SelectContext(ctx, &results, "SELECT `id`, `user_id`, `body`, `mime`, `created_at` FROM `posts` WHERE `user_id` = ? ORDER BY `created_at` DESC LIMIT ?", user.ID, postsPerPage*5)
	if err != nil {
		log.Print(err)
		return
	}

	posts, err := makePosts(ctx, results, getCSRFToken(r), false)
	if err != nil {
		log.Print(err)
		return
	}

	var counts struct {
		PostCount      int `db:"post_count"`
		CommentCount   int `db:"comment_count"`
		CommentedCount int `db:"commented_count"`
	}
	err = db.GetContext(ctx, &counts, `
		SELECT
			(SELECT COUNT(*) FROM posts WHERE user_id = ?) AS post_count,
			(SELECT COUNT(*) FROM comments WHERE user_id = ?) AS comment_count,
			(SELECT COUNT(*) FROM comments c INNER JOIN posts p ON c.post_id = p.id WHERE p.user_id = ?) AS commented_count
	`, user.ID, user.ID, user.ID)
	if err != nil {
		log.Print(err)
		return
	}

	me := getSessionUser(r)

	tmplUser.Execute(w, struct {
		Posts          []Post
		User           User
		PostCount      int
		CommentCount   int
		CommentedCount int
		Me             User
	}{posts, user, counts.PostCount, counts.CommentCount, counts.CommentedCount, me})
}

func getPosts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	m, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Print(err)
		return
	}
	maxCreatedAt := m.Get("max_created_at")
	if maxCreatedAt == "" {
		return
	}

	t, err := time.Parse(ISO8601Format, maxCreatedAt)
	if err != nil {
		log.Print(err)
		return
	}

	results := []Post{}
	err = db.SelectContext(ctx, &results, "SELECT `id`, `user_id`, `body`, `mime`, `created_at` FROM `posts` WHERE `created_at` <= ? ORDER BY `created_at` DESC LIMIT ?", t.Format(ISO8601Format), postsPerPage*5)
	if err != nil {
		log.Print(err)
		return
	}

	posts, err := makePosts(ctx, results, getCSRFToken(r), false)
	if err != nil {
		log.Print(err)
		return
	}

	if len(posts) == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	tmplPosts.Execute(w, posts)
}

func getPostsID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pidStr := r.PathValue("id")
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	results := []Post{}
	err = db.SelectContext(ctx, &results, "SELECT `id`, `user_id`, `body`, `mime`, `created_at` FROM `posts` WHERE `id` = ?", pid)
	if err != nil {
		log.Print(err)
		return
	}

	posts, err := makePosts(ctx, results, getCSRFToken(r), true)
	if err != nil {
		log.Print(err)
		return
	}

	if len(posts) == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	p := posts[0]

	me := getSessionUser(r)

	tmplPostID.Execute(w, struct {
		Post Post
		Me   User
	}{p, me})
}

func postIndex(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := getSessionUser(r)
	if !isLogin(me) {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	if r.FormValue("csrf_token") != getCSRFToken(r) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		session := getSession(r)
		session.Values["notice"] = "画像が必須です"
		session.Save(r, w)

		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	mime := ""
	if file != nil {
		// 投稿のContent-Typeからファイルのタイプを決定する
		contentType := header.Header["Content-Type"][0]
		if strings.Contains(contentType, "jpeg") {
			mime = "image/jpeg"
		} else if strings.Contains(contentType, "png") {
			mime = "image/png"
		} else if strings.Contains(contentType, "gif") {
			mime = "image/gif"
		} else {
			session := getSession(r)
			session.Values["notice"] = "投稿できる画像形式はjpgとpngとgifだけです"
			session.Save(r, w)

			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
	}

	filedata, err := io.ReadAll(file)
	if err != nil {
		log.Print(err)
		return
	}

	if len(filedata) > UploadLimit {
		session := getSession(r)
		session.Values["notice"] = "ファイルサイズが大きすぎます"
		session.Save(r, w)

		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	query := "INSERT INTO `posts` (`user_id`, `mime`, `imgdata`, `body`) VALUES (?,?,?,?)"
	result, err := db.ExecContext(
		ctx,
		query,
		me.ID,
		mime,
		[]byte{},
		r.FormValue("body"),
	)
	if err != nil {
		log.Print(err)
		return
	}

	pid, err := result.LastInsertId()
	if err != nil {
		log.Print(err)
		return
	}

	if err := savePostImage(int(pid), mime, filedata); err != nil {
		log.Print(err)
		return
	}

	http.Redirect(w, r, "/posts/"+strconv.FormatInt(pid, 10), http.StatusFound)
}

func getImage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pidStr := r.PathValue("id")
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	ext := r.PathValue("ext")
	mime := mimeFromExt(ext)
	if mime == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if data, err := os.ReadFile(imageFilePath(pid, ext)); err == nil {
		w.Header().Set("Content-Type", mime)
		if _, err := w.Write(data); err != nil {
			log.Print(err)
		}
		return
	}

	post := Post{}
	err = db.GetContext(ctx, &post, "SELECT `mime`, `imgdata` FROM `posts` WHERE `id` = ?", pid)
	if err != nil {
		log.Print(err)
		return
	}

	if ext == "jpg" && post.Mime == "image/jpeg" ||
		ext == "png" && post.Mime == "image/png" ||
		ext == "gif" && post.Mime == "image/gif" {
		w.Header().Set("Content-Type", post.Mime)
		if _, err := w.Write(post.Imgdata); err != nil {
			log.Print(err)
		}
		return
	}

	w.WriteHeader(http.StatusNotFound)
}

func postComment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := getSessionUser(r)
	if !isLogin(me) {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	if r.FormValue("csrf_token") != getCSRFToken(r) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	postID, err := strconv.Atoi(r.FormValue("post_id"))
	if err != nil {
		log.Print("post_idは整数のみです")
		return
	}

	query := "INSERT INTO `comments` (`post_id`, `user_id`, `comment`) VALUES (?,?,?)"
	_, err = db.ExecContext(ctx, query, postID, me.ID, r.FormValue("comment"))
	if err != nil {
		log.Print(err)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/posts/%d", postID), http.StatusFound)
}

func getAdminBanned(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := getSessionUser(r)
	if !isLogin(me) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	if me.Authority == 0 {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	users := []User{}
	err := db.SelectContext(ctx, &users, "SELECT `id`, `account_name`, `authority`, `del_flg`, `created_at` FROM `users` WHERE `authority` = 0 AND `del_flg` = 0 ORDER BY `created_at` DESC")
	if err != nil {
		log.Print(err)
		return
	}

	tmplBanned.Execute(w, struct {
		Users     []User
		Me        User
		CSRFToken string
	}{users, me, getCSRFToken(r)})
}

func postAdminBanned(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := getSessionUser(r)
	if !isLogin(me) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	if me.Authority == 0 {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	if r.FormValue("csrf_token") != getCSRFToken(r) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	query := "UPDATE `users` SET `del_flg` = ? WHERE `id` = ?"

	err := r.ParseForm()
	if err != nil {
		log.Print(err)
		return
	}

	for _, id := range r.Form["uid[]"] {
		db.ExecContext(ctx, query, 1, id)
	}

	http.Redirect(w, r, "/admin/banned", http.StatusFound)
}

func main() {
	host := os.Getenv("ISUCONP_DB_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("ISUCONP_DB_PORT")
	if port == "" {
		port = "3306"
	}
	_, err := strconv.Atoi(port)
	if err != nil {
		log.Fatalf("Failed to read DB port number from an environment variable ISUCONP_DB_PORT.\nError: %s", err.Error())
	}
	user := os.Getenv("ISUCONP_DB_USER")
	if user == "" {
		user = "root"
	}
	password := os.Getenv("ISUCONP_DB_PASSWORD")
	dbname := os.Getenv("ISUCONP_DB_NAME")
	if dbname == "" {
		dbname = "isuconp"
	}

	cfg := mysql.NewConfig()
	cfg.User = user
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:%s", host, port)
	cfg.DBName = dbname
	cfg.Params = map[string]string{
		"charset":           "utf8mb4",
		"interpolateParams": "true",
	}
	cfg.ParseTime = true
	cfg.Loc = time.Local
	dsn := cfg.FormatDSN()

	db, err = sqlx.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Failed to connect to DB: %s.", err.Error())
	}
	db.SetMaxOpenConns(80)
	db.SetMaxIdleConns(80)
	defer db.Close()

	if err := os.MkdirAll(imageDir, 0755); err != nil {
		log.Fatalf("Failed to create image dir: %s.", err.Error())
	}

	mustParseTemplates()

	r := chi.NewRouter()

	r.Get("/initialize", getInitialize)
	r.Get("/login", getLogin)
	r.Post("/login", postLogin)
	r.Get("/register", getRegister)
	r.Post("/register", postRegister)
	r.Get("/logout", getLogout)
	r.Get("/", getIndex)
	r.Get("/posts", getPosts)
	r.Get("/posts/{id}", getPostsID)
	r.Post("/", postIndex)
	r.Get("/image/{id}.{ext}", getImage)
	r.Post("/comment", postComment)
	r.Get("/admin/banned", getAdminBanned)
	r.Post("/admin/banned", postAdminBanned)
	r.Get(`/@{accountName:[0-9a-zA-Z_]+}`, getAccountName)
	r.Mount("/", http.FileServer(http.Dir("../public")))

	log.Fatal(http.ListenAndServe(":8080", r))
}
