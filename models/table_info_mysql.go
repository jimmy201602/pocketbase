//go:build mysql
package models

import "github.com/AlperRehaYAZGAN/postgresbase/tools/types"

type TableInfoRow struct {
	// the `db:"pk"` tag has special semantic so we cannot rename
	// the original field without specifying a custom mapper
	PK int

	Index        int           `db:"ORIGINAL_POSTION"`
	Name         string        `db:"COLUMN_NAME"`
	Type         string        `db:"DATA_TYPE"`
	NotNull      bool          `db:"notnull"` // bug need to be fixed
	DefaultValue types.JsonRaw `db:"COLUMN_DEFAULT"`
}
