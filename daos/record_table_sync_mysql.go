//go:build mysql
package daos

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/AlperRehaYAZGAN/postgresbase/models"
	"github.com/AlperRehaYAZGAN/postgresbase/models/schema"
	"github.com/AlperRehaYAZGAN/postgresbase/tools/dbutils"
	"github.com/AlperRehaYAZGAN/postgresbase/tools/security"
	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/pocketbase/dbx"
)

// SyncRecordTableSchema compares the two provided collections
// and applies the necessary related record table changes.
//
// If `oldCollection` is null, then only `newCollection` is used to create the record table.
func (dao *Dao) SyncRecordTableSchema(newCollection *models.Collection, oldCollection *models.Collection) error {
	return dao.RunInTransaction(func(txDao *Dao) error {
		// create
		// -----------------------------------------------------------
		if oldCollection == nil {
			cols := map[string]string{
				// !CHANGED: postgres snowflakeid and TIMESTAMP support
				// example: r0a1b2c3d4e5f6 or r0a1b2c3d4e5f6g
				schema.FieldNameId:      "VARCHAR(100) NOT NULL",
				schema.FieldNameCreated: "TIMESTAMP DEFAULT NOW() NOT NULL",
				schema.FieldNameUpdated: "TIMESTAMP DEFAULT NOW() NOT NULL",
			}

			if newCollection.IsAuth() {
				cols[schema.FieldNameUsername] = "VARCHAR(255) NOT NULL"
				cols[schema.FieldNameEmail] = "VARCHAR(100) NOT NULL"
				cols[schema.FieldNameEmailVisibility] = "TINYINT(1) NOT NULL DEFAULT 0"
				cols[schema.FieldNameVerified] = "TINYINT(1) NOT NULL DEFAULT 0"
				cols[schema.FieldNameTokenKey] = "VARCHAR(255) NOT NULL"
				cols[schema.FieldNamePasswordHash] = "TEXT NOT NULL"
				cols[schema.FieldNameLastResetSentAt] = "TEXT NOT NULL"
				cols[schema.FieldNameLastVerificationSentAt] = "TEXT NOT NULL"
			}

			if newCollection.Name == "_params" {
				cols["value"] = "JSON NOT NULL"
			}

			// ensure that the new collection has an id
			if !newCollection.HasId() {
				newCollection.RefreshId()
				newCollection.MarkAsNew()
			}

			tableName := newCollection.Name

			// add schema field definitions
			for _, field := range newCollection.Schema.Fields() {
				cols[field.Name] = field.ColDefinition()
			}

			// create table
			if _, err := txDao.DB().CreateTable(tableName, cols).Execute(); err != nil {
				return err
			}

			// add named unique index on the email and tokenKey columns
			if newCollection.IsAuth() {
				indexes := []string{
					fmt.Sprintf(`CREATE UNIQUE INDEX _%s_username_idx ON {{%s}} ([[username]]);`,newCollection.Id, tableName,),
					fmt.Sprintf(`CREATE UNIQUE INDEX _%s_email_idx ON {{%s}} ([[email]]);`,newCollection.Id, tableName,),
					fmt.Sprintf(`CREATE UNIQUE INDEX _%s_tokenKey_idx ON {{%s}} ([[tokenKey]]);`,newCollection.Id, tableName,),
				}
				for _,sql := range indexes{
					_, err := txDao.DB().NewQuery(sql).Execute()
					if err != nil {
						return err
					}
				}
			}

			return txDao.createCollectionIndexes(newCollection)
		}

		// update
		// -----------------------------------------------------------
		oldTableName := oldCollection.Name
		newTableName := newCollection.Name
		oldSchema := oldCollection.Schema
		newSchema := newCollection.Schema
		deletedFieldNames := []string{}
		renamedFieldNames := map[string]string{}

		// drop old indexes (if any)
		if err := txDao.dropCollectionIndex(oldCollection); err != nil {
			return err
		}

		// check for renamed table
		if !strings.EqualFold(oldTableName, newTableName) {
			_, err := txDao.DB().RenameTable("{{"+oldTableName+"}}", "{{"+newTableName+"}}").Execute()
			if err != nil {
				return err
			}
		}

		// check for deleted columns
		for _, oldField := range oldSchema.Fields() {
			if f := newSchema.GetFieldById(oldField.Id); f != nil {
				continue // exist
			}

			_, err := txDao.DB().DropColumn(newTableName, oldField.Name).Execute()
			if err != nil {
				return fmt.Errorf("failed to drop column %s - %w", oldField.Name, err)
			}

			deletedFieldNames = append(deletedFieldNames, oldField.Name)
		}

		// check for new or renamed columns
		toRename := map[string]string{}
		for _, field := range newSchema.Fields() {
			oldField := oldSchema.GetFieldById(field.Id)
			// Note:
			// We are using a temporary column name when adding or renaming columns
			// to ensure that there are no name collisions in case there is
			// names switch/reuse of existing columns (eg. name, title -> title, name).
			// This way we are always doing 1 more rename operation but it provides better dev experience.

			if oldField == nil {
				tempName := field.Name + security.PseudorandomString(5)
				toRename[tempName] = field.Name

				// add
				fmt.Println("add new column")
				_, err := txDao.DB().AddColumn(newTableName, tempName, field.ColDefinition()).Execute()
				if err != nil {
					return fmt.Errorf("failed to add column %s - %w", field.Name, err)
				}
			} else if oldField.Name != field.Name {
				tempName := field.Name + security.PseudorandomString(5)
				toRename[tempName] = field.Name

				//_, err := txDao.DB().RenameColumn(newTableName, oldField.Name, tempName).Execute()
				_, err := txDao.RenameColumn(newTableName, oldField.Name, tempName).Execute()
				if err != nil {
					return fmt.Errorf("failed to rename column %s - %w", oldField.Name, err)
				}

				renamedFieldNames[oldField.Name] = field.Name
			}
		}

		// set the actual columns name
		for tempName, actualName := range toRename {
			//_, err := txDao.DB().RenameColumn(newTableName, tempName, actualName).Execute()
			_, err := txDao.RenameColumn(newTableName, tempName, actualName).Execute()
			if err != nil {
				return err
			}
		}

		if err := txDao.normalizeSingleVsMultipleFieldChanges(newCollection, oldCollection); err != nil {
			return err
		}

		return txDao.createCollectionIndexes(newCollection)
	})
}

func (dao *Dao) normalizeSingleVsMultipleFieldChanges(newCollection, oldCollection *models.Collection) error {
	if newCollection.IsView() || oldCollection == nil {
		return nil // view or not an update
	}

	return dao.RunInTransaction(func(txDao *Dao) error {
		// !CHANGED: sqlite transaction pragmas removed
		// No equivalent pragma in PostgreSQL, so no changes needed.

		for _, newField := range newCollection.Schema.Fields() {
			// allow to continue even if there is no old field for the cases
			// when a new field is added and there are already inserted data
			var isOldMultiple bool
			if oldField := oldCollection.Schema.GetFieldById(newField.Id); oldField != nil {
				if opt, ok := oldField.Options.(schema.MultiValuer); ok {
					isOldMultiple = opt.IsMultiple()
				}
			}

			var isNewMultiple bool
			if opt, ok := newField.Options.(schema.MultiValuer); ok {
				isNewMultiple = opt.IsMultiple()
			}

			if isOldMultiple == isNewMultiple {
				continue // no change
			}

			// update the column definition by:
			// 1. inserting a new column with the new definition
			// 2. copy normalized values from the original column to the new one
			// 3. drop the original column
			// 4. rename the new column to the original column
			// -------------------------------------------------------

			originalName := newField.Name
			tempName := "_" + newField.Name + security.PseudorandomString(5)

			_, err := txDao.DB().AddColumn(newCollection.Name, tempName, newField.ColDefinition()).Execute()
			if err != nil {
				return err
			}

			var copyQuery *dbx.Query

			if !isOldMultiple && isNewMultiple {
				// single -> multiple (convert to array)
				copyQuery = txDao.DB().NewQuery(fmt.Sprintf(
					`UPDATE {{%s}} set [[%s]] = (
							CASE
								WHEN COALESCE([[%s]], '') = ''
								THEN '[]'
								ELSE (
									CASE
										WHEN JSON_VALID([[%s]]) AND JSON_TYPE([[%s]]) = 'ARRAY' THEN [[%s]]
										ELSE JSON_ARRAY([[%s]])
									END
								)
							END
						)`,
					newCollection.Name,
					tempName,
					originalName,
					originalName,
					originalName,
					originalName,
					originalName,
				))
			} else {
				// multiple -> single (keep only the last element)
				//
				// note: for file fields the actual file objects are not
				// deleted allowing additional custom handling via migration
				copyQuery = txDao.DB().NewQuery(fmt.Sprintf(
					`UPDATE {{%s}} set [[%s]] = (
						CASE
							WHEN COALESCE([[%s]], '[]') = '[]'
							THEN ''
							ELSE (
								CASE
									WHEN JSON_VALID([[%s]]) AND JSON_TYPE([[%s]]) = 'ARRAY'
									THEN COALESCE(REPLACE(JSON_EXTRACT([[%s]], '$[0]'), '"', ''), '')
									ELSE [[%s]]
								END
							)
						END
					)`,
					newCollection.Name,
					tempName,
					originalName,
					originalName,
					originalName,
					originalName,
					originalName,
				))
			}

			// copy the normalized values
			if _, err := copyQuery.Execute(); err != nil {
				return err
			}

			// drop the original column
			if _, err := txDao.DB().DropColumn(newCollection.Name, originalName).Execute(); err != nil {
				return err
			}

			// rename the new column back to the original
			// SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE table_name = 'test2' AND COLUMN_NAME = 'field'; get table type
			// https://stackoverflow.com/questions/1215368/how-to-get-the-mysql-table-columns-data-type
			//if _, err := txDao.DB().RenameColumn(newCollection.Name, tempName, originalName).Execute(); err != nil {
			//	return err
			//}
			if _, err := txDao.RenameColumn(newCollection.Name, tempName, originalName).Execute(); err != nil {
				return err
			}
		}

		// !CHANGED: sqlite transaction pragmas removed
		return nil
	})
}

func (dao *Dao) dropCollectionIndex(collection *models.Collection) error {
	if collection.IsView() {
		return nil // views don't have indexes
	}

	return dao.RunInTransaction(func(txDao *Dao) error {
		for _, raw := range collection.Indexes {
			parsed := dbutils.ParseIndex(raw)

			if !parsed.IsValid() {
				continue
			}

			//if _, err := txDao.DB().NewQuery(fmt.Sprintf("DROP INDEX IF EXISTS [[%s]]", parsed.IndexName)).Execute(); err != nil {
			//	return err
			//}
			indexes := []struct {
				TableName string
				IndexName  string
			}{}
			//dbx.Exists(dbx.NewExp("SELECT 1 FROM users WHERE status = 'active'"))
			query := txDao.DB().NewQuery("SELECT TABLE_NAME as TableName,INDEX_NAME as IndexName FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = {:TableName} AND INDEX_NAME = {:IndexName}")
			query.Bind(dbx.Params{"TableName":parsed.TableName, "IndexName": parsed.IndexName})
			err := query.All(&indexes)
			if err != nil{
				return err
			}
			if len(indexes) > 0 {
				if _, err := txDao.DB().NewQuery(fmt.Sprintf("DROP INDEX [[%s]] ON [[%s]]", parsed.IndexName, parsed.TableName)).Execute(); err != nil {
					return err
				}
			}
			return nil
		}

		return nil
	})
}

func (dao *Dao) createCollectionIndexes(collection *models.Collection) error {
	if collection.IsView() {
		return nil // views don't have indexes
	}

	return dao.RunInTransaction(func(txDao *Dao) error {
		// drop new indexes in case a duplicated index name is used
		if err := txDao.dropCollectionIndex(collection); err != nil {
			return err
		}

		// upsert new indexes
		//
		// note: we are returning validation errors because the indexes cannot be
		//       validated in a form, aka. before persisting the related collection
		//       record table changes
		errs := validation.Errors{}
		for i, idx := range collection.Indexes {
			parsed := dbutils.ParseIndex(idx)

			// ensure that the index is always for the current collection
			parsed.TableName = collection.Name

			if !parsed.IsValid() {
				errs[strconv.Itoa(i)] = validation.NewError(
					"validation_invalid_index_expression",
					"Invalid CREATE INDEX expression.",
				)
				continue
			}

			// bug need to be fixed
			sql := parsed.Build()
			if strings.HasPrefix(parsed.Build(),"CREATE INDEX"){
				sql = strings.Replace(parsed.Build(),"INDEX","FULLTEXT INDEX",1)
			}
			//if _, err := txDao.DB().NewQuery(parsed.Build()).Execute(); err != nil {
			if _, err := txDao.DB().NewQuery(sql).Execute(); err != nil {
				errs[strconv.Itoa(i)] = validation.NewError(
					"validation_invalid_index_expression",
					fmt.Sprintf("Failed to create index %s - %v.", parsed.IndexName, err.Error()),
				)
				continue
			}
		}

		if len(errs) > 0 {
			return validation.Errors{"indexes": errs}
		}

		return nil
	})
}
