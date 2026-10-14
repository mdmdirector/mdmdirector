package director

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/mdmdirector/mdmdirector/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const pendingSelect = `^SELECT "command_uuid","status","device_ud_id","request_type","identifier","manifest_url","content_hash","attempt_count","updated_at" FROM "commands" WHERE status IN \(\$1,\$2\)`

func pendingRows(n int) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"command_uuid", "status", "device_ud_id", "request_type", "updated_at"})
	for i := 0; i < n; i++ {
		rows.AddRow("cmd-"+string(rune('a'+i)), "", "udid-1", "InstallProfile", time.Unix(int64(i), 0))
	}
	return rows
}

func getPending(t *testing.T, rawQuery string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/command/pending?"+rawQuery, nil)
	rec := httptest.NewRecorder()
	GetPendingCommands(rec, req)
	return rec
}

func TestGetPendingCommands_DefaultLimit(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectQuery(pendingSelect+` ORDER BY updated_at, command_uuid LIMIT 101$`).
		WithArgs("", "NotNow").
		WillReturnRows(pendingRows(2))

	rec := getPending(t, "")

	require.Equal(t, http.StatusOK, rec.Code)
	var got []types.Command
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Len(t, got, 2)
	assert.Empty(t, rec.Header().Get("X-Next-Offset"))
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestGetPendingCommands_FiltersAndPaging(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	// limit=2 asks for 3 rows; getting 3 back means there is another page.
	mockSpy.ExpectQuery(pendingSelect+` AND device_ud_id = \$3 AND request_type = \$4 ORDER BY updated_at, command_uuid LIMIT 3 OFFSET 4$`).
		WithArgs("", "NotNow", "udid-1", "InstallProfile").
		WillReturnRows(pendingRows(3))

	rec := getPending(t, "udid=udid-1&request_type=InstallProfile&limit=2&offset=4")

	require.Equal(t, http.StatusOK, rec.Code)
	var got []types.Command
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Len(t, got, 2)
	assert.Equal(t, "6", rec.Header().Get("X-Next-Offset"))
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestGetPendingCommands_LimitCapped(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectQuery(pendingSelect+` ORDER BY updated_at, command_uuid LIMIT 1001$`).
		WithArgs("", "NotNow").
		WillReturnRows(pendingRows(0))

	rec := getPending(t, "limit=1000000")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `[]`, rec.Body.String())
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestGetPendingCommands_BadParams(t *testing.T) {
	for _, q := range []string{"limit=0", "limit=-1", "limit=abc", "offset=-5", "offset=x"} {
		t.Run(q, func(t *testing.T) {
			mockSpy, cleanup := setupMockDB(t)
			defer cleanup()

			rec := getPending(t, q)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.NoError(t, mockSpy.ExpectationsWereMet())
		})
	}
}

func deletePending(t *testing.T, rawQuery string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/command/pending/delete?"+rawQuery, nil)
	rec := httptest.NewRecorder()
	DeletePendingCommands(rec, req)
	return rec
}

// Without a udid the endpoint used to delete every pending row in the fleet.
func TestDeletePendingCommands_RequiresUDID(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	rec := deletePending(t, "")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestDeletePendingCommands_ForDevice(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`^DELETE FROM "commands" WHERE status IN \(\$1,\$2\) AND device_ud_id = \$3 AND request_type = \$4$`).
		WithArgs("", "NotNow", "udid-1", "InstallProfile").
		WillReturnResult(sqlmock.NewResult(0, 7))
	mockSpy.ExpectCommit()

	rec := deletePending(t, "udid=udid-1&request_type=InstallProfile")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"deleted":7}`, rec.Body.String())
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestDeletePendingCommands_AllOptIn(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`^DELETE FROM "commands" WHERE status IN \(\$1,\$2\)$`).
		WithArgs("", "NotNow").
		WillReturnResult(sqlmock.NewResult(0, 42))
	mockSpy.ExpectCommit()

	rec := deletePending(t, "all=true")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"deleted":42}`, rec.Body.String())
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestDeletePendingCommands_DBError(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectBegin()
	mockSpy.ExpectExec(`DELETE FROM "commands"`).WillReturnError(assert.AnError)
	mockSpy.ExpectRollback()

	rec := deletePending(t, "udid=udid-1")

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}
