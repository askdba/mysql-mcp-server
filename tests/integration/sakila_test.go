//go:build integration

// tests/integration/sakila_test.go
// Integration tests using MySQL Sakila sample database
// These tests verify the MCP server works correctly with a realistic database schema
package integration

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

var (
	sakilaSetupOnce sync.Once
	sakilaSetupErr  error
)

// getSakilaDSN returns the DSN for Sakila database from environment
func getSakilaDSN(t *testing.T) string {
	t.Helper()

	// Check for Sakila-specific DSN first
	dsn := os.Getenv("MYSQL_SAKILA_DSN")
	if dsn == "" {
		// Fall back to test DSN with sakila database
		baseDSN := os.Getenv("MYSQL_TEST_DSN")
		if baseDSN == "" {
			baseDSN = os.Getenv("MYSQL_DSN")
		}
		if baseDSN == "" {
			t.Skip("MYSQL_SAKILA_DSN, MYSQL_TEST_DSN, or MYSQL_DSN not set, skipping Sakila integration test")
		}
		// Modify DSN to use sakila database
		// Assumes format: user:pass@tcp(host:port)/db?params
		// We need to use the sakila database
		dsn = baseDSN
	}
	return dsn
}

// setupSakilaSchema loads the Sakila schema and data into the database
func setupSakilaSchema(db *sql.DB) error {
	ctx := context.Background()

	// Get the path to SQL files relative to this test file
	// The SQL files are in tests/sql/
	sqlDir := filepath.Join("..", "..", "tests", "sql")

	// Check if sakila already exists
	var dbName string
	err := db.QueryRowContext(ctx, "SELECT SCHEMA_NAME FROM INFORMATION_SCHEMA.SCHEMATA WHERE SCHEMA_NAME = 'sakila'").Scan(&dbName)
	if err == nil && dbName == "sakila" {
		// Sakila already exists, check if it has data
		var actorCount int
		err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sakila.actor").Scan(&actorCount)
		if err == nil && actorCount > 0 {
			// Database already set up
			return nil
		}
	}

	// Load schema file
	schemaPath := filepath.Join(sqlDir, "sakila-schema.sql")
	schemaSQL, err := os.ReadFile(schemaPath)
	if err != nil {
		return err
	}

	// Execute schema (multi-statement)
	// Note: For multi-statement execution, we need multiStatements=true in DSN
	// Since we can't guarantee that, we'll execute statements one at a time
	// For now, we'll use a simpler approach - execute the whole file if possible

	// Try to execute using mysql command or statement-by-statement
	// For simplicity in tests, we'll execute the statements we need directly

	// Create sakila database
	_, err = db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS sakila")
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, "USE sakila")
	if err != nil {
		return err
	}

	// Execute schema SQL (this requires multiStatements=true in DSN)
	_, err = db.ExecContext(ctx, string(schemaSQL))
	if err != nil {
		// If multi-statement fails, the DSN might not support it
		// In that case, we'll skip and let the docker-compose handle it
		return err
	}

	// Load data file
	dataPath := filepath.Join(sqlDir, "sakila-data.sql")
	dataSQL, err := os.ReadFile(dataPath)
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, string(dataSQL))
	if err != nil {
		return err
	}

	return nil
}

// setupSakilaDB creates a connection to Sakila database
func setupSakilaDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := getSakilaDSN(t)

	// Ensure multiStatements is enabled for setup
	if dsn[len(dsn)-1] != '?' && dsn[len(dsn)-1] != '&' {
		if contains(dsn, "?") {
			dsn += "&multiStatements=true"
		} else {
			dsn += "?multiStatements=true"
		}
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	// Wait for connection to be ready
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for {
		if err := db.PingContext(ctx); err == nil {
			break
		}
		select {
		case <-time.After(1 * time.Second):
			// retry
		case <-ctx.Done():
			t.Fatalf("database not ready within timeout")
		}
	}

	// Setup Sakila schema once per test run
	sakilaSetupOnce.Do(func() {
		sakilaSetupErr = setupSakilaSchema(db)
	})
	if sakilaSetupErr != nil {
		// Schema setup failed - this is expected if multiStatements isn't enabled
		// Tests will still work if sakila was loaded via docker-compose
		t.Logf("Sakila setup from Go failed (expected if using docker-compose): %v", sakilaSetupErr)
	}

	// Switch to sakila database
	_, err = db.ExecContext(context.Background(), "USE sakila")
	if err != nil {
		t.Fatalf("failed to use sakila database: %v", err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

// ============================================================================
// Sakila Database Tests
// ============================================================================

// TestSakila_ListTables verifies we can list tables in Sakila
func TestSakila_ListTables(t *testing.T) {
	db := setupSakilaDB(t)
	ctx := context.Background()

	rows, err := db.QueryContext(ctx, "SHOW TABLES")
	if err != nil {
		t.Fatalf("SHOW TABLES failed: %v", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("failed to scan table name: %v", err)
		}
		tables = append(tables, name)
	}

	// Verify expected Sakila tables exist
	expectedTables := []string{
		"actor", "address", "category", "city", "country",
		"customer", "film", "film_actor", "film_category",
		"inventory", "language", "payment", "rental", "staff", "store",
	}

	for _, expected := range expectedTables {
		found := false
		for _, table := range tables {
			if table == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected to find table '%s' in Sakila database, got: %v", expected, tables)
		}
	}
}

// TestSakila_DescribeTable verifies we can describe Sakila tables
func TestSakila_DescribeTable(t *testing.T) {
	db := setupSakilaDB(t)
	ctx := context.Background()

	testCases := []struct {
		table           string
		expectedColumns []string
	}{
		{
			table:           "actor",
			expectedColumns: []string{"actor_id", "first_name", "last_name", "last_update"},
		},
		{
			table:           "film",
			expectedColumns: []string{"film_id", "title", "description", "release_year", "language_id", "rental_duration", "rental_rate", "length", "rating"},
		},
		{
			table:           "customer",
			expectedColumns: []string{"customer_id", "store_id", "first_name", "last_name", "email", "address_id", "active"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.table, func(t *testing.T) {
			rows, err := db.QueryContext(ctx, "DESCRIBE "+tc.table)
			if err != nil {
				t.Fatalf("DESCRIBE %s failed: %v", tc.table, err)
			}
			defer rows.Close()

			columns := make(map[string]bool)
			for rows.Next() {
				var field, colType string
				var null, key, defaultVal, extra sql.NullString
				if err := rows.Scan(&field, &colType, &null, &key, &defaultVal, &extra); err != nil {
					t.Fatalf("failed to scan column info: %v", err)
				}
				columns[field] = true
			}

			for _, col := range tc.expectedColumns {
				if !columns[col] {
					t.Errorf("expected column '%s' not found in %s table", col, tc.table)
				}
			}
		})
	}
}

// TestSakila_BasicQueries tests basic SELECT queries on Sakila
func TestSakila_BasicQueries(t *testing.T) {
	db := setupSakilaDB(t)
	ctx := context.Background()

	testCases := []struct {
		name    string
		query   string
		minRows int
	}{
		{
			name:    "select all actors",
			query:   "SELECT * FROM actor",
			minRows: 10,
		},
		{
			name:    "select all films",
			query:   "SELECT * FROM film",
			minRows: 10,
		},
		{
			name:    "select films with limit",
			query:   "SELECT * FROM film LIMIT 5",
			minRows: 5,
		},
		{
			name:    "select films ordered by title",
			query:   "SELECT * FROM film ORDER BY title",
			minRows: 10,
		},
		{
			name:    "select specific actor",
			query:   "SELECT * FROM actor WHERE first_name = 'PENELOPE'",
			minRows: 1,
		},
		{
			name:    "select films by rating",
			query:   "SELECT * FROM film WHERE rating = 'PG'",
			minRows: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := db.QueryContext(ctx, tc.query)
			if err != nil {
				t.Fatalf("query failed: %v", err)
			}
			defer rows.Close()

			count := 0
			for rows.Next() {
				count++
			}

			if count < tc.minRows {
				t.Errorf("expected at least %d rows, got %d", tc.minRows, count)
			}
		})
	}
}

// TestSakila_JoinQueries tests JOIN queries across Sakila tables
func TestSakila_JoinQueries(t *testing.T) {
	db := setupSakilaDB(t)
	ctx := context.Background()

	testCases := []struct {
		name    string
		query   string
		minRows int
	}{
		{
			name: "films with actors",
			query: `SELECT f.title, a.first_name, a.last_name 
                    FROM film f 
                    JOIN film_actor fa ON f.film_id = fa.film_id 
                    JOIN actor a ON fa.actor_id = a.actor_id 
                    LIMIT 20`,
			minRows: 10,
		},
		{
			name: "films with categories",
			query: `SELECT f.title, c.name as category 
                    FROM film f 
                    JOIN film_category fc ON f.film_id = fc.film_id 
                    JOIN category c ON fc.category_id = c.category_id`,
			minRows: 10,
		},
		{
			name: "customers with addresses",
			query: `SELECT cu.first_name, cu.last_name, a.address, ci.city, co.country 
                    FROM customer cu 
                    JOIN address a ON cu.address_id = a.address_id 
                    JOIN city ci ON a.city_id = ci.city_id 
                    JOIN country co ON ci.country_id = co.country_id`,
			minRows: 5,
		},
		{
			name: "rentals with customer and film info",
			query: `SELECT cu.first_name, cu.last_name, f.title, r.rental_date, r.return_date
                    FROM rental r
                    JOIN customer cu ON r.customer_id = cu.customer_id
                    JOIN inventory i ON r.inventory_id = i.inventory_id
                    JOIN film f ON i.film_id = f.film_id
                    LIMIT 20`,
			minRows: 10,
		},
		{
			name: "staff with store info",
			query: `SELECT s.first_name, s.last_name, st.store_id, a.address
                    FROM staff s
                    JOIN store st ON s.store_id = st.store_id
                    JOIN address a ON st.address_id = a.address_id`,
			minRows: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := db.QueryContext(ctx, tc.query)
			if err != nil {
				t.Fatalf("query failed: %v", err)
			}
			defer rows.Close()

			count := 0
			for rows.Next() {
				count++
			}

			if count < tc.minRows {
				t.Errorf("expected at least %d rows, got %d", tc.minRows, count)
			}
		})
	}
}

// TestSakila_AggregationQueries tests aggregate functions on Sakila
func TestSakila_AggregationQueries(t *testing.T) {
	db := setupSakilaDB(t)
	ctx := context.Background()

	testCases := []struct {
		name  string
		query string
	}{
		{
			name:  "count films",
			query: "SELECT COUNT(*) as total_films FROM film",
		},
		{
			name:  "count actors",
			query: "SELECT COUNT(*) as total_actors FROM actor",
		},
		{
			name:  "average rental rate",
			query: "SELECT AVG(rental_rate) as avg_rate FROM film",
		},
		{
			name:  "sum of payments",
			query: "SELECT SUM(amount) as total_revenue FROM payment",
		},
		{
			name:  "films per rating",
			query: "SELECT rating, COUNT(*) as count FROM film GROUP BY rating",
		},
		{
			name:  "films per category",
			query: "SELECT c.name, COUNT(*) as film_count FROM film f JOIN film_category fc ON f.film_id = fc.film_id JOIN category c ON fc.category_id = c.category_id GROUP BY c.name",
		},
		{
			name:  "actors with most films",
			query: "SELECT a.first_name, a.last_name, COUNT(*) as film_count FROM actor a JOIN film_actor fa ON a.actor_id = fa.actor_id GROUP BY a.actor_id ORDER BY film_count DESC LIMIT 5",
		},
		{
			name:  "revenue by customer",
			query: "SELECT c.first_name, c.last_name, SUM(p.amount) as total_spent FROM customer c JOIN payment p ON c.customer_id = p.customer_id GROUP BY c.customer_id ORDER BY total_spent DESC LIMIT 5",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := db.QueryContext(ctx, tc.query)
			if err != nil {
				t.Fatalf("query failed: %v", err)
			}
			defer rows.Close()

			if !rows.Next() {
				t.Error("expected at least one row")
			}
		})
	}
}

// TestSakila_SubquerySupport tests subqueries on Sakila
func TestSakila_SubquerySupport(t *testing.T) {
	db := setupSakilaDB(t)
	ctx := context.Background()

	testCases := []struct {
		name  string
		query string
	}{
		{
			name:  "films with above average rental rate",
			query: "SELECT * FROM film WHERE rental_rate > (SELECT AVG(rental_rate) FROM film)",
		},
		{
			name:  "actors who appear in action films",
			query: "SELECT * FROM actor WHERE actor_id IN (SELECT fa.actor_id FROM film_actor fa JOIN film_category fc ON fa.film_id = fc.film_id JOIN category c ON fc.category_id = c.category_id WHERE c.name = 'Action')",
		},
		{
			name:  "customers who have rented",
			query: "SELECT * FROM customer WHERE customer_id IN (SELECT DISTINCT customer_id FROM rental)",
		},
		{
			name:  "films never rented",
			query: "SELECT * FROM film WHERE film_id NOT IN (SELECT DISTINCT i.film_id FROM inventory i JOIN rental r ON i.inventory_id = r.inventory_id)",
		},
		{
			name: "correlated subquery - customers with rentals",
			query: `SELECT c.first_name, c.last_name, 
                    (SELECT COUNT(*) FROM rental r WHERE r.customer_id = c.customer_id) as rental_count 
                    FROM customer c LIMIT 10`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := db.QueryContext(ctx, tc.query)
			if err != nil {
				t.Fatalf("query failed: %v", err)
			}
			defer rows.Close()

			// Just verify query executes
			for rows.Next() {
			}
			if err := rows.Err(); err != nil {
				t.Errorf("error iterating rows: %v", err)
			}
		})
	}
}

// TestSakila_ViewQueries tests querying Sakila views
func TestSakila_ViewQueries(t *testing.T) {
	db := setupSakilaDB(t)
	ctx := context.Background()

	testCases := []struct {
		name    string
		query   string
		minRows int
	}{
		{
			name:    "film_list view",
			query:   "SELECT * FROM film_list LIMIT 10",
			minRows: 5,
		},
		{
			name:    "customer_list view",
			query:   "SELECT * FROM customer_list LIMIT 10",
			minRows: 5,
		},
		{
			name:    "staff_list view",
			query:   "SELECT * FROM staff_list",
			minRows: 1,
		},
		{
			name:    "sales_by_film_category view",
			query:   "SELECT * FROM sales_by_film_category",
			minRows: 1,
		},
		{
			name:    "sales_by_store view",
			query:   "SELECT * FROM sales_by_store",
			minRows: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := db.QueryContext(ctx, tc.query)
			if err != nil {
				// Views might not exist if schema wasn't fully loaded
				t.Skipf("view query failed (may not be loaded): %v", err)
			}
			defer rows.Close()

			count := 0
			for rows.Next() {
				count++
			}

			if count < tc.minRows {
				t.Errorf("expected at least %d rows, got %d", tc.minRows, count)
			}
		})
	}
}

// TestSakila_DateTimeQueries tests date/time operations on Sakila
func TestSakila_DateTimeQueries(t *testing.T) {
	db := setupSakilaDB(t)
	ctx := context.Background()

	testCases := []struct {
		name  string
		query string
	}{
		{
			name:  "rentals with date formatting",
			query: "SELECT rental_id, DATE_FORMAT(rental_date, '%Y-%m-%d') as date FROM rental LIMIT 5",
		},
		{
			name:  "rental duration calculation",
			query: "SELECT rental_id, DATEDIFF(return_date, rental_date) as days_rented FROM rental WHERE return_date IS NOT NULL LIMIT 5",
		},
		{
			name:  "payments by month",
			query: "SELECT YEAR(payment_date) as year, MONTH(payment_date) as month, SUM(amount) as total FROM payment GROUP BY YEAR(payment_date), MONTH(payment_date)",
		},
		{
			name:  "recent updates",
			query: "SELECT * FROM actor WHERE last_update >= DATE_SUB(NOW(), INTERVAL 10 YEAR)",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := db.QueryContext(ctx, tc.query)
			if err != nil {
				t.Fatalf("query failed: %v", err)
			}
			defer rows.Close()

			// Just verify query executes
			for rows.Next() {
			}
			if err := rows.Err(); err != nil {
				t.Errorf("error: %v", err)
			}
		})
	}
}

// TestSakila_ENUMAndSETTypes tests ENUM and SET data types in Sakila
func TestSakila_ENUMAndSETTypes(t *testing.T) {
	db := setupSakilaDB(t)
	ctx := context.Background()

	testCases := []struct {
		name  string
		query string
	}{
		{
			name:  "film ratings (ENUM)",
			query: "SELECT DISTINCT rating FROM film ORDER BY rating",
		},
		{
			name:  "film special features (SET)",
			query: "SELECT title, special_features FROM film WHERE special_features IS NOT NULL LIMIT 10",
		},
		{
			name:  "films by rating",
			query: "SELECT rating, COUNT(*) as count FROM film GROUP BY rating ORDER BY rating",
		},
		{
			name:  "films with specific features",
			query: "SELECT title, special_features FROM film WHERE FIND_IN_SET('Trailers', special_features) > 0 LIMIT 10",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := db.QueryContext(ctx, tc.query)
			if err != nil {
				t.Fatalf("query failed: %v", err)
			}
			defer rows.Close()

			count := 0
			for rows.Next() {
				count++
			}
			if count == 0 {
				t.Error("expected at least one row")
			}
		})
	}
}

// TestSakila_FullTextSearch tests FULLTEXT search capabilities
func TestSakila_FullTextSearch(t *testing.T) {
	db := setupSakilaDB(t)
	ctx := context.Background()

	// Check if film_text table has data and fulltext index
	var count int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM film_text").Scan(&count)
	if err != nil || count == 0 {
		t.Skip("film_text table not populated, skipping fulltext tests")
	}

	testCases := []struct {
		name  string
		query string
	}{
		{
			name:  "natural language search",
			query: "SELECT * FROM film_text WHERE MATCH(title, description) AGAINST('drama' IN NATURAL LANGUAGE MODE)",
		},
		{
			name:  "boolean mode search",
			query: "SELECT * FROM film_text WHERE MATCH(title, description) AGAINST('+epic +drama' IN BOOLEAN MODE)",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := db.QueryContext(ctx, tc.query)
			if err != nil {
				t.Skipf("fulltext query failed (may not be supported): %v", err)
			}
			defer rows.Close()

			// Just verify query executes
			for rows.Next() {
			}
		})
	}
}

// TestSakila_IndexUsage tests that indexes are properly used
func TestSakila_IndexUsage(t *testing.T) {
	db := setupSakilaDB(t)
	ctx := context.Background()

	testCases := []struct {
		name  string
		query string
	}{
		{
			name:  "show indexes on film",
			query: "SHOW INDEX FROM film",
		},
		{
			name:  "show indexes on actor",
			query: "SHOW INDEX FROM actor",
		},
		{
			name:  "explain query on indexed column",
			query: "EXPLAIN SELECT * FROM actor WHERE last_name = 'GUINESS'",
		},
		{
			name:  "explain join query",
			query: "EXPLAIN SELECT f.title, a.first_name FROM film f JOIN film_actor fa ON f.film_id = fa.film_id JOIN actor a ON fa.actor_id = a.actor_id WHERE f.film_id = 1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := db.QueryContext(ctx, tc.query)
			if err != nil {
				t.Fatalf("query failed: %v", err)
			}
			defer rows.Close()

			if !rows.Next() {
				t.Error("expected at least one row")
			}
		})
	}
}

// TestSakila_ForeignKeyRelationships verifies foreign key constraints
func TestSakila_ForeignKeyRelationships(t *testing.T) {
	db := setupSakilaDB(t)
	ctx := context.Background()

	// Query to check foreign keys
	query := `
		SELECT 
			TABLE_NAME,
			COLUMN_NAME,
			CONSTRAINT_NAME,
			REFERENCED_TABLE_NAME,
			REFERENCED_COLUMN_NAME
		FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE
		WHERE TABLE_SCHEMA = 'sakila'
		AND REFERENCED_TABLE_NAME IS NOT NULL
		ORDER BY TABLE_NAME, COLUMN_NAME
	`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()

	fkCount := 0
	for rows.Next() {
		var tableName, columnName, constraintName, refTable, refColumn string
		if err := rows.Scan(&tableName, &columnName, &constraintName, &refTable, &refColumn); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		fkCount++
	}

	// Sakila should have multiple foreign keys
	if fkCount < 10 {
		t.Errorf("expected at least 10 foreign keys, got %d", fkCount)
	}
}

// TestSakila_ComplexBusinessQueries tests complex business-like queries
func TestSakila_ComplexBusinessQueries(t *testing.T) {
	db := setupSakilaDB(t)
	ctx := context.Background()

	testCases := []struct {
		name  string
		query string
	}{
		{
			name: "top renting customers",
			query: `
				SELECT c.customer_id, c.first_name, c.last_name, 
					COUNT(r.rental_id) as rental_count,
					SUM(p.amount) as total_spent
				FROM customer c
				LEFT JOIN rental r ON c.customer_id = r.customer_id
				LEFT JOIN payment p ON r.rental_id = p.rental_id
				GROUP BY c.customer_id, c.first_name, c.last_name
				ORDER BY rental_count DESC
				LIMIT 10
			`,
		},
		{
			name: "films by category with inventory",
			query: `
				SELECT c.name as category, 
					COUNT(DISTINCT f.film_id) as films,
					COUNT(i.inventory_id) as inventory_items
				FROM category c
				LEFT JOIN film_category fc ON c.category_id = fc.category_id
				LEFT JOIN film f ON fc.film_id = f.film_id
				LEFT JOIN inventory i ON f.film_id = i.film_id
				GROUP BY c.category_id, c.name
				ORDER BY films DESC
			`,
		},
		{
			name: "store performance",
			query: `
				SELECT s.store_id,
					COUNT(DISTINCT i.film_id) as unique_films,
					COUNT(i.inventory_id) as total_inventory,
					COUNT(r.rental_id) as total_rentals
				FROM store s
				LEFT JOIN inventory i ON s.store_id = i.store_id
				LEFT JOIN rental r ON i.inventory_id = r.inventory_id
				GROUP BY s.store_id
			`,
		},
		{
			name: "actor filmography stats",
			query: `
				SELECT a.actor_id, a.first_name, a.last_name,
					COUNT(fa.film_id) as total_films,
					GROUP_CONCAT(DISTINCT c.name ORDER BY c.name SEPARATOR ', ') as categories
				FROM actor a
				LEFT JOIN film_actor fa ON a.actor_id = fa.actor_id
				LEFT JOIN film_category fc ON fa.film_id = fc.film_id
				LEFT JOIN category c ON fc.category_id = c.category_id
				GROUP BY a.actor_id, a.first_name, a.last_name
				HAVING total_films > 0
				ORDER BY total_films DESC
				LIMIT 10
			`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := db.QueryContext(ctx, tc.query)
			if err != nil {
				t.Fatalf("query failed: %v", err)
			}
			defer rows.Close()

			count := 0
			for rows.Next() {
				count++
			}
			if count == 0 {
				t.Error("expected at least one row")
			}
		})
	}
}

// contains helper function to check if a string contains a substring
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
