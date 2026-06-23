package main

import (
	"fmt"
	"log"
	"os"
	"path"
	"strconv"

	mysql "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

const imageDir = "/home/isucon/private_isu/webapp/images"

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

func main() {
	host := os.Getenv("ISUCONP_DB_HOST")
	if host == "" {
		host = "localhost"
	}
	user := os.Getenv("ISUCONP_DB_USER")
	if user == "" {
		user = "isuconp"
	}
	password := os.Getenv("ISUCONP_DB_PASSWORD")
	if password == "" {
		password = "isuconp"
	}
	dbname := os.Getenv("ISUCONP_DB_NAME")
	if dbname == "" {
		dbname = "isuconp"
	}

	cfg := mysql.NewConfig()
	cfg.User = user
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = host + ":3306"
	cfg.DBName = dbname
	cfg.Params = map[string]string{"charset": "utf8mb4"}

	db, err := sqlx.Open("mysql", cfg.FormatDSN())
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := os.MkdirAll(imageDir, 0755); err != nil {
		log.Fatal(err)
	}

	rows, err := db.Queryx("SELECT `id`, `mime`, `imgdata` FROM `posts` WHERE LENGTH(`imgdata`) > 0")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	exported := 0
	for rows.Next() {
		var id int
		var mime string
		var imgdata []byte
		if err := rows.Scan(&id, &mime, &imgdata); err != nil {
			log.Fatal(err)
		}

		ext := extFromMime(mime)
		if ext == "" {
			continue
		}
		fp := path.Join(imageDir, strconv.Itoa(id)+"."+ext)
		if _, err := os.Stat(fp); err == nil {
			continue
		}
		if err := os.WriteFile(fp, imgdata, 0644); err != nil {
			log.Fatal(err)
		}
		exported++
		imgdata = nil // 明示的に参照を外して GC を促す
	}
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("exported %d images to %s\n", exported, imageDir)
}
