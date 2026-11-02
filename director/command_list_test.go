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

func commandRows(ids ...string) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"command_uuid", "status"})
	for _, id := range ids {
		rows.AddRow(id, "Acknowledged")
	}
	return rows
}

func mustDecode(t *testing.T, rec *httptest.ResponseRecorder) []types.Command {
	t.Helper()
	var out []types.Command
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out
}

func get(h http.HandlerFunc, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestCursorRoundTrip(t *testing.T) {
	ts := time.Date(2026, 10, 14, 12, 0, 0, 123456000, time.UTC).Format(time.RFC3339Nano)
	parts, err := decodeCursor(encodeCursor(ts, "cmd-1"), 2)
	require.NoError(t, err)
	assert.Equal(t, []string{ts, "cmd-1"}, parts)

	_, err = decodeCursor(encodeCursor("only-one"), 2)
	assert.Error(t, err)
	_, err = decodeCursor("not base64!", 1)
	assert.Error(t, err)
}

func TestGetAllCommands_FirstPageSetsCursor(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	// limit=2 asks for 3 rows; the third says there is another page.
	mockSpy.ExpectQuery(`^SELECT \* FROM "commands" ORDER BY command_uuid LIMIT 3$`).
		WillReturnRows(commandRows("a", "b", "c"))

	rec := get(GetAllCommands, "/command?limit=2")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, mustDecode(t, rec), 2)
	assert.Equal(t, encodeCursor("b"), rec.Header().Get("X-Next-Cursor"))
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestGetAllCommands_CursorContinues(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectQuery(`^SELECT \* FROM "commands" WHERE command_uuid > \$1 ORDER BY command_uuid LIMIT 101$`).
		WithArgs("b").
		WillReturnRows(commandRows("c"))

	rec := get(GetAllCommands, "/command?cursor="+encodeCursor("b"))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, mustDecode(t, rec), 1)
	assert.Empty(t, rec.Header().Get("X-Next-Cursor"), "last page has no cursor")
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestGetErrorCommands_CursorAndCap(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectQuery(`^SELECT \* FROM "commands" WHERE status = 'Error' AND command_uuid > \$1 ORDER BY command_uuid LIMIT 1001$`).
		WithArgs("b").
		WillReturnRows(commandRows())

	rec := get(GetErrorCommands, "/command/error?limit=5000&cursor="+encodeCursor("b"))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "[]", rec.Body.String())
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestListCommands_BadParams(t *testing.T) {
	_, cleanup := setupMockDB(t)
	defer cleanup()

	for _, q := range []string{"limit=0", "limit=-1", "limit=x", "cursor=%21%21", "cursor=" + encodeCursor("a", "b")} {
		assert.Equal(t, http.StatusBadRequest, get(GetAllCommands, "/command?"+q).Code, q)
		assert.Equal(t, http.StatusBadRequest, get(GetErrorCommands, "/command/error?"+q).Code, q)
	}
}

func TestGetPendingCommands_CursorUsesKeyset(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	after := time.Date(2026, 10, 14, 12, 0, 0, 123456000, time.UTC)
	mockSpy.ExpectQuery(pendingSelect+` AND \(updated_at, command_uuid\) > \(\$1, \$2\) ORDER BY updated_at, command_uuid LIMIT 3$`).
		WithArgs(after, "cmd-a").
		WillReturnRows(pendingRows(3))

	rec := get(GetPendingCommands, "/command/pending?limit=2&cursor="+encodeCursor(after.Format(time.RFC3339Nano), "cmd-a"))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, mustDecode(t, rec), 2)
	assert.NotEmpty(t, rec.Header().Get("X-Next-Cursor"))
	assert.Empty(t, rec.Header().Get("X-Next-Offset"), "cursor mode does not hand out offsets")
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestGetPendingCommands_OffsetModeSetsBothHeaders(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	mockSpy.ExpectQuery(pendingSelect + ` ORDER BY updated_at, command_uuid LIMIT 3$`).
		WillReturnRows(pendingRows(3))

	rec := get(GetPendingCommands, "/command/pending?limit=2")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "2", rec.Header().Get("X-Next-Offset"))
	parts, err := decodeCursor(rec.Header().Get("X-Next-Cursor"), 2)
	require.NoError(t, err)
	assert.Equal(t, "cmd-b", parts[1], "cursor points at the last row returned")
	assert.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestGetPendingCommands_CursorParamErrors(t *testing.T) {
	_, cleanup := setupMockDB(t)
	defer cleanup()

	good := encodeCursor(time.Now().UTC().Format(time.RFC3339Nano), "cmd-a")
	for _, q := range []string{
		"cursor=" + good + "&offset=10",
		"cursor=" + encodeCursor("cmd-a"),
		"cursor=" + encodeCursor("yesterday", "cmd-a"),
	} {
		assert.Equal(t, http.StatusBadRequest, get(GetPendingCommands, "/command/pending?"+q).Code, q)
	}
}
