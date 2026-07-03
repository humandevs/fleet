package tables

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUp_20260703000000(t *testing.T) {
	db := applyUpToPrev(t)

	applyNext(t, db)

	// Table should exist and be empty.
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM host_integration_status`).Scan(&count))
	require.Equal(t, 0, count)

	// A host is required to satisfy the FK.
	hostID := execNoErrLastID(t, db, `INSERT INTO hosts (osquery_host_id) VALUES ('his-host-1')`)

	// A coverage cell round-trips.
	_, err := db.Exec(
		`INSERT INTO host_integration_status (host_id, source, category, state) VALUES (?, 'bitdefender', 'av', 'protected')`,
		hostID,
	)
	require.NoError(t, err)

	var state string
	require.NoError(t, db.QueryRow(
		`SELECT state FROM host_integration_status WHERE host_id = ? AND source = 'bitdefender' AND category = 'av'`,
		hostID,
	).Scan(&state))
	require.Equal(t, "protected", state)

	// Duplicate (host_id, source, category) is rejected by the primary key.
	_, err = db.Exec(
		`INSERT INTO host_integration_status (host_id, source, category, state) VALUES (?, 'bitdefender', 'av', 'at_risk')`,
		hostID,
	)
	require.Error(t, err)

	// Deleting the host cascades.
	_, err = db.Exec(`DELETE FROM hosts WHERE id = ?`, hostID)
	require.NoError(t, err)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM host_integration_status`).Scan(&count))
	require.Equal(t, 0, count)
}
