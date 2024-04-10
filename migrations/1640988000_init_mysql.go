//go:build mysql
// Package migrations contains the system PocketBase DB migrations.
package migrations

import (
	"path/filepath"
	"runtime"

	"github.com/AlperRehaYAZGAN/postgresbase/daos"
	"github.com/AlperRehaYAZGAN/postgresbase/models"
	"github.com/AlperRehaYAZGAN/postgresbase/models/schema"
	"github.com/AlperRehaYAZGAN/postgresbase/models/settings"
	"github.com/AlperRehaYAZGAN/postgresbase/tools/migrate"
	"github.com/AlperRehaYAZGAN/postgresbase/tools/types"
	"github.com/pocketbase/dbx"
)

var AppMigrations migrate.MigrationsList

// Register is a short alias for `AppMigrations.Register()`
// that is usually used in external/user defined migrations.
func Register(
	up func(db dbx.Builder) error,
	down func(db dbx.Builder) error,
	optFilename ...string,
) {
	var optFiles []string
	if len(optFilename) > 0 {
		optFiles = optFilename
	} else {
		_, path, _, _ := runtime.Caller(1)
		optFiles = append(optFiles, filepath.Base(path))
	}
	AppMigrations.Register(up, down, optFiles...)
}

func init() {
	AppMigrations.Register(func(db dbx.Builder) error {
		tables := []string{
			`
			CREATE TABLE {{_admins}} (
				[[id]]        		VARCHAR(100) NOT NULL,
				[[avatar]]          INT(11) NOT NULL DEFAULT '0',
				[[email]]           VARCHAR(100) NOT NULL,
				[[tokenKey]]        VARCHAR(255) NOT NULL,
				[[passwordHash]]    TEXT NOT NULL,
				[[lastResetSentAt]] TEXT NOT NULL,
				[[created]]         TIMESTAMP DEFAULT NOW() NOT NULL,
				[[updated]]         TIMESTAMP DEFAULT NOW() NOT NULL,
				PRIMARY KEY ([[id]]),
				UNIQUE KEY [[email]] ([[email]]),
				UNIQUE KEY [[tokenKey]] ([[tokenKey]])
			);
			`,
			`
			CREATE TABLE {{_collections}} (
				[[id]]		   VARCHAR(100) NOT NULL,
				[[system]]     TINYINT(1) NOT NULL,
				[[type]]       VARCHAR(255) NOT NULL DEFAULT 'base',
				[[name]]       VARCHAR(255) NOT NULL,
				[[schema]]     JSON NOT NULL,
				[[indexes]]    JSON NOT NULL,
				[[listRule]]   TEXT,
				[[viewRule]]   TEXT,
				[[createRule]] TEXT,
				[[updateRule]] TEXT,
				[[deleteRule]] TEXT,
				[[options]]    JSON NOT NULL,
				[[created]]    TIMESTAMP DEFAULT NOW() NOT NULL,
				[[updated]]    TIMESTAMP DEFAULT NOW() NOT NULL,
				PRIMARY KEY ([[id]]),
				UNIQUE KEY [[name]] ([[name]])
			);`,
			`
			CREATE TABLE {{_params}} (
				[[id]]      VARCHAR(100) NOT NULL,
				[[key]]     VARCHAR(255) NOT NULL,
				[[value]]   JSON DEFAULT NULL,
				[[created]] TIMESTAMP DEFAULT NOW() NOT NULL,
				[[updated]] TIMESTAMP DEFAULT NOW() NOT NULL,
				PRIMARY KEY ([[id]]),
				UNIQUE KEY [[key]] ([[key]])
			);`,
			`
			CREATE TABLE {{_externalAuths}} (
				[[id]]           VARCHAR(100) NOT NULL,
				[[collectionId]] VARCHAR(255) NOT NULL,
				[[recordId]]     VARCHAR(255) NOT NULL,
				[[provider]]     VARCHAR(255) NOT NULL,
				[[providerId]]   VARCHAR(255) NOT NULL,
				[[created]]      TIMESTAMP DEFAULT NOW() NOT NULL,
				[[updated]]      TIMESTAMP DEFAULT NOW() NOT NULL,
				PRIMARY KEY ([[id]]),
				UNIQUE KEY [[_externalAuths_record_provider_idx]] ([[collectionId]],[[recordId]],[[provider]]),
				UNIQUE KEY [[_externalAuths_collection_provider_idx]] ([[collectionId]],[[provider]],[[providerId]]),
				CONSTRAINT [[_externalAuths_collectionId]] FOREIGN KEY ([[collectionId]]) REFERENCES [[_collections]] ([[id]]) ON DELETE CASCADE ON UPDATE CASCADE
			);`,
		}

		for _,sql := range tables{
			_, tablesErr := db.NewQuery(sql).Execute()
			if tablesErr != nil {
				return tablesErr
			}
		}

		dao := daos.New(db)

		// inserts default settings
		// -----------------------------------------------------------
		defaultSettings := settings.New()
		if err := dao.SaveSettings(defaultSettings); err != nil {
			return err
		}

		// inserts the system users collection
		// -----------------------------------------------------------
		usersCollection := &models.Collection{}
		usersCollection.MarkAsNew()
		usersCollection.Id = "_pb_users_auth_"
		usersCollection.Name = "users"
		usersCollection.Type = models.CollectionTypeAuth
		usersCollection.ListRule = types.Pointer("id = @request.auth.id")
		usersCollection.ViewRule = types.Pointer("id = @request.auth.id")
		usersCollection.CreateRule = types.Pointer("")
		usersCollection.UpdateRule = types.Pointer("id = @request.auth.id")
		usersCollection.DeleteRule = types.Pointer("id = @request.auth.id")

		// set auth options
		usersCollection.SetOptions(models.CollectionAuthOptions{
			ManageRule:        nil,
			AllowOAuth2Auth:   true,
			AllowUsernameAuth: true,
			AllowEmailAuth:    true,
			MinPasswordLength: 8,
			RequireEmail:      false,
		})

		// set optional default fields
		usersCollection.Schema = schema.NewSchema(
			&schema.SchemaField{
				Id:      "users_name",
				Type:    schema.FieldTypeText,
				Name:    "name",
				Options: &schema.TextOptions{},
			},
			&schema.SchemaField{
				Id:   "users_avatar",
				Type: schema.FieldTypeFile,
				Name: "avatar",
				Options: &schema.FileOptions{
					MaxSelect: 1,
					MaxSize:   5242880,
					MimeTypes: []string{
						"image/jpeg",
						"image/png",
						"image/svg+xml",
						"image/gif",
						"image/webp",
					},
				},
			},
		)

		return dao.SaveCollection(usersCollection)
	}, func(db dbx.Builder) error {
		tables := []string{
			"users",
			"_externalAuths",
			"_params",
			"_collections",
			"_admins",
		}

		for _, name := range tables {
			if _, err := db.DropTable(name).Execute(); err != nil {
				return err
			}
		}

		return nil
	})
}
