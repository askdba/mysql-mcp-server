//go:build integration

// cmd/mysql-mcp-server/toolpack_integration_test.go
// Parity of the mysql_mcp SQL tool pack (RFC 0001 §5.1) with the embedded SQL, against a
// real server: every catalog tool must return the same output either way.
//
// Needs MYSQL_TEST_DSN for an account that can install the pack (CREATE, DROP, CREATE
// ROUTINE, ALTER ROUTINE on mysql_mcp.*, see tests/sql/mcp_test_user.sql) and can create
// tables in its default database. Uses the sakila schema when it is visible.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/askdba/mysql-mcp-server/internal/config"
	"github.com/askdba/mysql-mcp-server/internal/toolpack"
)

func TestToolPackParity(t *testing.T) {
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSQL_TEST_DSN not set")
	}
	mc, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	testDB := mc.DBName
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	if err := toolpack.Install(ctx, db); err != nil {
		t.Fatalf("install-schema: %v", err)
	}
	// Installing twice is the upgrade path.
	if err := toolpack.Install(ctx, db); err != nil {
		t.Fatalf("re-install: %v", err)
	}
	if v := toolpack.Detect(ctx, db); v != toolpack.Version {
		t.Fatalf("Detect = %q, want %q", v, toolpack.Version)
	}

	// Fixtures: a partitioned table, a foreign key, a view, a trigger, a procedure and
	// (MySQL 9+) VECTOR columns. A stored function needs SUPER with binary logging on, so
	// functions come from sakila when it is visible.
	cleanup := func() {
		for _, stmt := range []string{
			"DROP VIEW IF EXISTS tp_parent_v",
			"DROP PROCEDURE IF EXISTS tp_proc",
			"DROP TABLE IF EXISTS tp_child, tp_parent, tp_parts, tp_vec",
		} {
			_, _ = db.ExecContext(context.Background(), stmt)
		}
	}
	cleanup()
	defer cleanup()
	for _, stmt := range []string{
		"CREATE TABLE tp_parent (id INT PRIMARY KEY, name VARCHAR(20), KEY ix_name (name))",
		"CREATE TABLE tp_child (id INT PRIMARY KEY, parent_id INT, CONSTRAINT fk_tp_parent FOREIGN KEY (parent_id) REFERENCES tp_parent(id) ON DELETE CASCADE)",
		"CREATE TABLE tp_parts (id INT, d DATE) PARTITION BY RANGE (YEAR(d)) (PARTITION p2024 VALUES LESS THAN (2025), PARTITION pmax VALUES LESS THAN MAXVALUE)",
		"CREATE VIEW tp_parent_v AS SELECT id, name FROM tp_parent",
		"CREATE PROCEDURE tp_proc() SELECT 1",
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	// Like stored functions, triggers need SUPER when binary logging is on (MySQL default),
	// and sakila's are hidden from an account without the TRIGGER privilege there.
	triggerFixture := false
	if _, err := db.ExecContext(ctx, "CREATE TRIGGER tp_parent_bi BEFORE INSERT ON tp_parent FOR EACH ROW SET NEW.name = TRIM(NEW.name)"); err == nil {
		triggerFixture = true
	}
	vectorTable := false
	if _, err := db.ExecContext(ctx, "CREATE TABLE tp_vec (id INT PRIMARY KEY, emb VECTOR(3), emb2 VECTOR(4))"); err == nil {
		vectorTable = true // MySQL 9+
	}

	sakila := ""
	var n int
	if db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA = 'sakila'").Scan(&n) == nil && n > 0 {
		sakila = "sakila"
	}
	richDB := testDB
	if sakila != "" {
		richDB = sakila
	}

	// One connection, "default", whose pack flag the test flips.
	cm := NewConnectionManager()
	cm.connections["default"] = db
	cm.configs["default"] = config.ConnectionConfig{Name: "default", DSN: dsn}
	cm.serverTypes["default"] = cm.detectServerType(ctx, db) // vector_info checks it
	cm.activeConn = "default"
	oldCM, oldCfg, oldMax, oldTimeout := connManager, cfg, maxRows, queryTimeout
	connManager, cfg, maxRows, queryTimeout = cm, &config.Config{MaxRows: 200}, 200, 30*time.Second
	defer func() { connManager, cfg, maxRows, queryTimeout = oldCM, oldCfg, oldMax, oldTimeout }()
	setPack := func(on bool) {
		cm.mu.Lock()
		defer cm.mu.Unlock()
		if on {
			cm.toolPacks["default"] = toolpack.Version
		} else {
			delete(cm.toolPacks, "default")
		}
	}

	// both runs call with the pack off and on and returns the JSON of output and error.
	both := func(call func() (any, error)) (embedded, pack string) {
		res := func() string {
			out, err := call()
			b, _ := json.Marshal(out)
			if err != nil {
				return "error: " + err.Error()
			}
			return string(b)
		}
		setPack(false)
		embedded = res()
		setPack(true)
		pack = res()
		// A pack call that hit 1305/1370 would fall back to the embedded SQL and pass the
		// comparison without running a procedure; the fallback clears the marker.
		if cm.ToolPackOf("default") == "" {
			pack = "error: pack mode fell back to the embedded SQL"
		}
		setPack(false)
		return embedded, pack
	}
	// same requires identical output in both modes. Calls on known objects must also
	// succeed (an error shared by both modes would otherwise pass); only no_such_* cases
	// may fail, identically.
	outputs := map[string]string{}
	same := func(name string, call func() (any, error)) {
		t.Helper()
		e, p := both(call)
		// The one known difference: SHOW CREATE TABLE on a view omits the schema name when
		// the view is in the session's current database. A procedure runs with mysql_mcp as
		// its current database, so pack mode always qualifies it; embedded mode does not
		// for views in the DSN's default database.
		if strings.HasPrefix(name, "show_create_table ") && strings.Contains(p, " VIEW `"+testDB+"`.") {
			p = strings.ReplaceAll(p, "`"+testDB+"`.", "")
		}
		switch {
		case e != p:
			t.Errorf("%s differs:\n embedded: %.600s\n pack:     %.600s", name, e, p)
		case strings.HasPrefix(e, "error:") && !strings.Contains(name, "no_such"):
			t.Errorf("%s failed in both modes: %.600s", name, e)
		case testing.Verbose():
			t.Logf("%s: same (%d bytes)", name, len(e))
		}
		outputs[name] = e
	}
	// nonEmpty fails when an earlier same() call for a known object listed nothing.
	nonEmpty := func(name, field string) {
		t.Helper()
		if out, ok := outputs[name]; !ok || strings.Contains(out, `"`+field+`":[]`) || strings.Contains(out, `"`+field+`":null`) {
			t.Errorf("%s: expected a non-empty %s, got %.300s", name, field, out)
		}
	}
	wrap := func(out any, err error) (any, error) { return out, err }

	same("list_databases", func() (any, error) {
		_, o, err := toolListDatabases(ctx, nil, ListDatabasesInput{})
		return wrap(o, err)
	})
	for _, d := range []string{testDB, richDB, "no_such_db"} {
		d := d
		same("list_tables "+d, func() (any, error) {
			_, o, err := toolListTables(ctx, nil, ListTablesInput{Database: d})
			return wrap(o, err)
		})
		same("list_views "+d, func() (any, error) {
			_, o, err := toolListViews(ctx, nil, ListViewsInput{Database: d})
			return wrap(o, err)
		})
		same("list_triggers "+d, func() (any, error) {
			_, o, err := toolListTriggers(ctx, nil, ListTriggersInput{Database: d})
			return wrap(o, err)
		})
		same("list_procedures "+d, func() (any, error) {
			_, o, err := toolListProcedures(ctx, nil, ListProceduresInput{Database: d})
			return wrap(o, err)
		})
		same("list_functions "+d, func() (any, error) {
			_, o, err := toolListFunctions(ctx, nil, ListFunctionsInput{Database: d})
			return wrap(o, err)
		})
		same("database_size "+d, func() (any, error) {
			_, o, err := toolDatabaseSize(ctx, nil, DatabaseSizeInput{Database: d})
			return wrap(o, err)
		})
		same("table_size "+d, func() (any, error) {
			_, o, err := toolTableSize(ctx, nil, TableSizeInput{Database: d})
			return wrap(o, err)
		})
		same("foreign_keys "+d, func() (any, error) {
			_, o, err := toolForeignKeys(ctx, nil, ForeignKeysInput{Database: d})
			return wrap(o, err)
		})
		same("vector_info "+d, func() (any, error) {
			_, o, err := toolVectorInfo(ctx, nil, VectorInfoInput{Database: d})
			return wrap(o, err)
		})
	}
	same("database_size all", func() (any, error) { _, o, err := toolDatabaseSize(ctx, nil, DatabaseSizeInput{}); return wrap(o, err) })
	nonEmpty("list_views "+testDB, "views")
	nonEmpty("list_procedures "+testDB, "procedures")
	nonEmpty("foreign_keys "+testDB, "foreign_keys")
	if triggerFixture {
		nonEmpty("list_triggers "+testDB, "triggers")
	}
	if sakila != "" {
		nonEmpty("list_views "+sakila, "views")
		nonEmpty("list_procedures "+sakila, "procedures")
		nonEmpty("list_functions "+sakila, "functions")
	}
	// The tool reports VECTOR columns only where it detects support (MySQL 9+).
	if vectorTable && strings.Contains(outputs["vector_info "+testDB], `"vector_support":true`) {
		nonEmpty("vector_info "+testDB, "columns")
	}

	tables := [][2]string{{testDB, "tp_parent"}, {testDB, "tp_child"}, {testDB, "tp_parts"}, {testDB, "tp_parent_v"}, {testDB, "no_such_table"}}
	if sakila != "" {
		tables = append(tables, [2]string{sakila, "film"}, [2]string{sakila, "payment"}, [2]string{sakila, "actor_info"})
	}
	for _, dt := range tables {
		d, tb := dt[0], dt[1]
		label := d + "." + tb
		same("describe_table "+label, func() (any, error) {
			_, o, err := toolDescribeTable(ctx, nil, DescribeTableInput{Database: d, Table: tb})
			return wrap(o, err)
		})
		same("show_create_table "+label, func() (any, error) {
			_, o, err := toolShowCreateTable(ctx, nil, ShowCreateTableInput{Database: d, Table: tb})
			return wrap(o, err)
		})
		same("list_indexes "+label, func() (any, error) {
			_, o, err := toolListIndexes(ctx, nil, ListIndexesInput{Database: d, Table: tb})
			return wrap(o, err)
		})
		same("list_partitions "+label, func() (any, error) {
			_, o, err := toolListPartitions(ctx, nil, ListPartitionsInput{Database: d, Table: tb})
			return wrap(o, err)
		})
		same("table_size "+label, func() (any, error) {
			_, o, err := toolTableSize(ctx, nil, TableSizeInput{Database: d, Table: tb})
			return wrap(o, err)
		})
		same("foreign_keys "+label, func() (any, error) {
			_, o, err := toolForeignKeys(ctx, nil, ForeignKeysInput{Database: d, Table: tb})
			return wrap(o, err)
		})
		same("vector_info "+label, func() (any, error) {
			_, o, err := toolVectorInfo(ctx, nil, VectorInfoInput{Database: d, Table: tb})
			return wrap(o, err)
		})
	}

	for _, s := range []SearchSchemaInput{{Pattern: "%film%"}, {Pattern: "%id"}, {Pattern: "TP_%"}, {Pattern: "%id", Database: richDB}, {Pattern: "%name%", Database: testDB}} {
		s := s
		same("search_schema "+s.Pattern+" in "+s.Database, func() (any, error) { _, o, err := toolSearchSchema(ctx, nil, s); return wrap(o, err) })
	}
	// A small row limit cuts both at the same place.
	maxRows = 3
	same("search_schema %id limit 3", func() (any, error) {
		_, o, err := toolSearchSchema(ctx, nil, SearchSchemaInput{Pattern: "%id"})
		return wrap(o, err)
	})
	same("list_tables limit 3", func() (any, error) {
		_, o, err := toolListTables(ctx, nil, ListTablesInput{Database: richDB})
		return wrap(o, err)
	})
	maxRows = 200

	same("list_status Innodb_page_size", func() (any, error) {
		_, o, err := toolListStatus(ctx, nil, ListStatusInput{Pattern: "Innodb_page_size"})
		return wrap(o, err)
	})
	same("list_variables max_conn%", func() (any, error) {
		_, o, err := toolListVariables(ctx, nil, ListVariablesInput{Pattern: "max_conn%"})
		return wrap(o, err)
	})
	same("list_variables MAX_CONN% (case)", func() (any, error) {
		_, o, err := toolListVariables(ctx, nil, ListVariablesInput{Pattern: "MAX_CONN%"})
		return wrap(o, err)
	})
	// Values such as Uptime change between two reads: compare names for the full lists.
	names := func(call func() ([]string, error)) func() (any, error) {
		return func() (any, error) { v, err := call(); return wrap(v, err) }
	}
	same("list_variables (names)", names(func() ([]string, error) {
		_, o, err := toolListVariables(ctx, nil, ListVariablesInput{})
		var out []string
		for _, v := range o.Variables {
			out = append(out, v.Name)
		}
		return out, err
	}))
	same("list_status (names)", names(func() ([]string, error) {
		_, o, err := toolListStatus(ctx, nil, ListStatusInput{})
		var out []string
		for _, v := range o.Variables {
			out = append(out, v.Name)
		}
		return out, err
	}))
	same("process_list (shape)", names(func() ([]string, error) {
		_, o, err := toolProcessList(ctx, nil, ProcessListInput{})
		return []string{o.Note, strings.Repeat("x", min(len(o.Processes), 1))}, err
	}))

	// The pack really is used, and a dropped procedure (for example an interrupted
	// upgrade) falls back to the embedded SQL and stops using the pack on the connection.
	if _, err := db.ExecContext(ctx, "DROP PROCEDURE mysql_mcp.list_databases"); err != nil {
		t.Fatal(err)
	}
	setPack(false)
	_, embeddedDBs, err := toolListDatabases(ctx, nil, ListDatabasesInput{})
	if err != nil {
		t.Fatal(err)
	}
	setPack(true)
	_, fallback, err := toolListDatabases(ctx, nil, ListDatabasesInput{})
	if err != nil || !reflect.DeepEqual(fallback, embeddedDBs) {
		t.Errorf("pack mode without mysql_mcp.list_databases: %+v %v, want the embedded result %+v", fallback, err, embeddedDBs)
	}
	if v := cm.ToolPackOf("default"); v != "" {
		t.Errorf("pack still in use after a missing procedure: %q", v)
	}
	setPack(false)
	if err := toolpack.Install(ctx, db); err != nil {
		t.Fatal(err)
	}

	// Turning the pack off by configuration means it is never detected.
	if v := detectToolPack(ctx, db, config.ToolPackOff); v != "" {
		t.Errorf("MYSQL_MCP_TOOL_PACK=off still detected %q", v)
	}
	if v := detectToolPack(ctx, db, config.ToolPackAuto); v != toolpack.Version {
		t.Errorf("auto detect = %q", v)
	}
}
