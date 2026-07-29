package director

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/taskq/v3"
)

// fakeQueue is a taskq.Queue that only records the messages passed to Add, so tests can
// inspect the delay and dedup name pushAll assigned to each one.
type fakeQueue struct {
	taskq.Queue
	added []*taskq.Message
}

func (q *fakeQueue) Name() string { return "fake" }

func (q *fakeQueue) String() string { return "fakeQueue" }

func (q *fakeQueue) Add(msg *taskq.Message) error {
	q.added = append(q.added, msg)
	return nil
}

// expectDeviceScan stubs the `db.DB.Find(&devices).Scan(&devices)` in pushAll, returning
// `count` devices whose info-command timestamps are all zero (so deviceNeedsPush is true).
// `Find(&x).Scan(&x)` runs the same statement twice, so both runs are stubbed.
func expectDeviceScan(mockSpy sqlmock.Sqlmock, count int) []string {
	udids := make([]string, 0, count)
	for i := 0; i < count; i++ {
		udids = append(udids, fmt.Sprintf("UDID-%04d", i))
	}

	newRows := func() *sqlmock.Rows {
		rows := sqlmock.NewRows([]string{"ud_id", "serial_number"})
		for i, udid := range udids {
			rows.AddRow(udid, fmt.Sprintf("SERIAL%04d", i))
		}
		return rows
	}

	mockSpy.ExpectQuery(`SELECT \* FROM "devices"`).WillReturnRows(newRows())
	mockSpy.ExpectQuery(`SELECT \* FROM "devices"`).WillReturnRows(newRows())
	return udids
}

func TestPushAllSpreadsDelaysWithoutSleeping(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	const deviceCount = 200
	udids := expectDeviceScan(mockSpy, deviceCount)

	onceIn := 60 * time.Minute
	pushSpread := 30 * time.Minute

	queue := &fakeQueue{}
	task := taskq.RegisterTask(&taskq.TaskOptions{
		Name:    "push-spread-test",
		Handler: func(string) error { return nil },
	})

	start := time.Now()
	require.NoError(t, pushAll(queue, task, onceIn, pushSpread))
	elapsed := time.Since(start)

	// No sleeps: enqueuing 200 devices must not take anywhere near the old
	// 500ms-per-batch / 1-minute-per-chunk pacing.
	assert.Less(t, elapsed, 5*time.Second, "pushAll should not sleep while enqueuing")

	require.Len(t, queue.added, deviceCount)

	seenNames := make(map[string]struct{}, deviceCount)
	distinctDelays := make(map[time.Duration]struct{})
	for i, msg := range queue.added {
		assert.Equal(t, udids[i], msg.Args[0], "messages enqueued in device order")

		// Delivery is delayed by at least onceIn and at most onceIn+pushSpread.
		assert.GreaterOrEqual(t, msg.Delay, onceIn, "delay below onceIn for %s", udids[i])
		assert.Less(t, msg.Delay, onceIn+pushSpread, "delay beyond the spread window for %s", udids[i])

		// OnceInPeriod's dedup name must survive the SetDelay override.
		require.NotEmpty(t, msg.Name, "dedup name missing for %s", udids[i])
		_, dup := seenNames[msg.Name]
		assert.False(t, dup, "duplicate dedup name for %s", udids[i])
		seenNames[msg.Name] = struct{}{}

		distinctDelays[msg.Delay] = struct{}{}
	}

	// Jitter should actually spread the pushes out rather than clumping them.
	assert.Greater(t, len(distinctDelays), 1, "expected pushes to be spread across the window")

	require.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestPushAllZeroSpreadUsesOnceInDelay(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	expectDeviceScan(mockSpy, 3)

	onceIn := 15 * time.Minute
	queue := &fakeQueue{}
	task := taskq.RegisterTask(&taskq.TaskOptions{
		Name:    "push-zero-spread-test",
		Handler: func(string) error { return nil },
	})

	require.NoError(t, pushAll(queue, task, onceIn, 0))

	require.Len(t, queue.added, 3)
	for _, msg := range queue.added {
		assert.Equal(t, onceIn, msg.Delay)
		assert.NotEmpty(t, msg.Name)
	}

	require.NoError(t, mockSpy.ExpectationsWereMet())
}

func TestPushAllNoDevicesEnqueuesNothing(t *testing.T) {
	mockSpy, cleanup := setupMockDB(t)
	defer cleanup()

	expectDeviceScan(mockSpy, 0)

	queue := &fakeQueue{}
	task := taskq.RegisterTask(&taskq.TaskOptions{
		Name:    "push-no-devices-test",
		Handler: func(string) error { return nil },
	})

	require.NoError(t, pushAll(queue, task, time.Minute, time.Minute))
	assert.Empty(t, queue.added)
	require.NoError(t, mockSpy.ExpectationsWereMet())
}

// TestPushAllHasNoSleeps guards the invariant that made the control-plane scan slow: any
// time.Sleep in pushAll blocks the scan while it holds the control-plane Redis lock.
func TestPushAllHasNoSleeps(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "schedule_push.go", nil, 0)
	require.NoError(t, err)

	var fn *ast.FuncDecl
	for _, decl := range file.Decls {
		if d, ok := decl.(*ast.FuncDecl); ok && d.Name.Name == "pushAll" {
			fn = d
			break
		}
	}
	require.NotNil(t, fn, "pushAll not found in schedule_push.go")

	ast.Inspect(fn, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if ok && pkg.Name == "time" && sel.Sel.Name == "Sleep" {
			t.Errorf("pushAll must not sleep: found time.Sleep at %s", fset.Position(sel.Pos()))
		}
		return true
	})
}
