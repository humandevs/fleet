package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20260703000000, Down_20260703000000)
}

func Up_20260703000000(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE host_integration_status (
  host_id    INT UNSIGNED NOT NULL,
  source     VARCHAR(64) NOT NULL,
  category   VARCHAR(32) NOT NULL,
  state      VARCHAR(32) NOT NULL,
  detail     VARCHAR(255) NOT NULL DEFAULT '',
  updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (host_id, source, category),
  CONSTRAINT fk_host_integration_status_host FOREIGN KEY (host_id)
    REFERENCES hosts (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return fmt.Errorf("creating host_integration_status table: %w", err)
	}
	return nil
}

func Down_20260703000000(tx *sql.Tx) error {
	return nil
}
