//go:build mysql

package core

import (
	"os"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"github.com/pocketbase/dbx"
)

func connectDB(dbPath string) (*dbx.DB, error) {
	if strings.Contains(dbPath, "logs.db") {
		return dbx.MustOpen("mysql", os.Getenv("LOGS_DATABASE"))
	}
	return dbx.MustOpen("mysql", os.Getenv("DATABASE"))
}
