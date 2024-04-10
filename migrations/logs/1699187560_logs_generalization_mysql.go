//go:build mysql
package logs

import (
	"github.com/pocketbase/dbx"
)

func init() {
	LogsMigrations.Register(func(db dbx.Builder) error {
		//if _, err := db.DropTable("_requests").Execute(); err != nil {
		//	return err
		//}

		tables := []string{
			`
			CREATE TABLE {{_logs}} (
				[[id]]      VARCHAR(100) NOT NULL,
				[[level]]   INT(11) NOT NULL DEFAULT '0',
				[[message]] TEXT NOT NULL,
				[[data]]    JSON NOT NULL,
				[[created]] TIMESTAMP DEFAULT NOW() NOT NULL,
				[[updated]] TIMESTAMP DEFAULT NOW() NOT NULL
			);`,
			`CREATE INDEX _logs_level_idx on {{_logs}} ([[level]]);`,
			`CREATE INDEX _logs_message_idx on {{_logs}} ([[message]]);`,
			`CREATE INDEX _logs_created_hour_idx on {{_logs}} (immutable_date_trunc('hour', [[created]]));`,
		}
		for _,sql :=range tables{
			_, err := db.NewQuery(sql).Execute()

			return err
		}
		return nil
	}, func(db dbx.Builder) error {
		if _, err := db.DropTable("_logs").Execute(); err != nil {
			return err
		}

		_, err := db.NewQuery(`
			CREATE TABLE {{_requests}} (
				[[id]]        VARCHAR(100) NOT NULL,
				[[url]]       TEXT NOT NULL,
				[[method]]    VARCHAR(255) NOT NULL DEFAULT 'get',
				[[status]]    INT(11) NOT NULL DEFAULT '200',
				[[auth]]      VARCHAR(255) NOT NULL DEFAULT 'guest',
				[[ip]]         VARCHAR(255) NOT NULL DEFAULT '127.0.0.1',
				[[referer]]   TEXT NOT NULL,
				[[userAgent]] TEXT NOT NULL,
				[[meta]]      JSON NOT NULL,
				[[created]]   TIMESTAMP DEFAULT NOW() NOT NULL,
				[[updated]]   TIMESTAMP DEFAULT NOW() NOT NULL
			);

			CREATE INDEX _request_status_idx on {{_requests}} ([[status]]);
			CREATE INDEX _request_auth_idx on {{_requests}} ([[auth]]);
			CREATE INDEX _request_ip_idx on {{_requests}} ([[ip]]);
			CREATE INDEX _request_created_hour_idx on {{_requests}} (date_trunc('hour', [[created]]) IMMUTABLE);
		`).Execute()

		return err
	})
}
