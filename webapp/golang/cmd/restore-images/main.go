package main

import (
	"fmt"
	"log"
	"os"
	"path"
	"strconv"
	"strings"

	mysql "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

const imageDir = "/home/isucon/private_isu/webapp/images"

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

	entries, err := os.ReadDir(imageDir)
	if err != nil {
		log.Fatal(err)
	}

	restored := 0
	skipped := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		dot := strings.LastIndex(name, ".")
		if dot <= 0 {
			continue
		}
		id, err := strconv.Atoi(name[:dot])
		if err != nil || id > 10000 {
			continue
		}
		fp := path.Join(imageDir, name)
		imgdata, err := os.ReadFile(fp)
		if err != nil {
			log.Fatal(err)
		}
		res, err := db.Exec(
			"UPDATE `posts` SET `imgdata` = ? WHERE `id` = ? AND LENGTH(`imgdata`) = 0",
			imgdata, id,
		)
		if err != nil {
			log.Fatal(err)
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			restored++
		} else {
			skipped++
		}
		imgdata = nil
	}
	fmt.Printf("restored %d posts, skipped %d (already had imgdata)\n", restored, skipped)
}
