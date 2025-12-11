// cmd/mysql-mcp-server/types_test.go
package main

import (
	"encoding/json"
	"testing"
)

// ===== Core Types JSON Tests =====

func TestListDatabasesInputJSON(t *testing.T) {
	input := ListDatabasesInput{}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	if string(data) != "{}" {
		t.Errorf("expected '{}', got '%s'", string(data))
	}
}

func TestDatabaseInfoJSON(t *testing.T) {
	info := DatabaseInfo{Name: "testdb"}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded DatabaseInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Name != "testdb" {
		t.Errorf("expected name 'testdb', got '%s'", decoded.Name)
	}
}

func TestListDatabasesOutputJSON(t *testing.T) {
	output := ListDatabasesOutput{
		Databases: []DatabaseInfo{
			{Name: "db1"},
			{Name: "db2"},
		},
	}
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ListDatabasesOutput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(decoded.Databases) != 2 {
		t.Errorf("expected 2 databases, got %d", len(decoded.Databases))
	}
}

func TestListTablesInputJSON(t *testing.T) {
	input := ListTablesInput{Database: "testdb"}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ListTablesInput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Database != "testdb" {
		t.Errorf("expected database 'testdb', got '%s'", decoded.Database)
	}
}

func TestTableInfoJSON(t *testing.T) {
	info := TableInfo{Name: "users"}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded TableInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Name != "users" {
		t.Errorf("expected name 'users', got '%s'", decoded.Name)
	}
}

func TestDescribeTableInputJSON(t *testing.T) {
	input := DescribeTableInput{Database: "testdb", Table: "users"}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded DescribeTableInput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Database != "testdb" || decoded.Table != "users" {
		t.Errorf("unexpected values: database='%s', table='%s'", decoded.Database, decoded.Table)
	}
}

func TestColumnInfoJSON(t *testing.T) {
	info := ColumnInfo{
		Name:      "id",
		Type:      "int",
		Null:      "NO",
		Key:       "PRI",
		Default:   "",
		Extra:     "auto_increment",
		Comment:   "Primary key",
		Collation: "",
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ColumnInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Name != "id" {
		t.Errorf("expected name 'id', got '%s'", decoded.Name)
	}
	if decoded.Key != "PRI" {
		t.Errorf("expected key 'PRI', got '%s'", decoded.Key)
	}
}

func TestRunQueryInputJSON(t *testing.T) {
	maxRows := 100
	input := RunQueryInput{
		SQL:      "SELECT * FROM users",
		MaxRows:  &maxRows,
		Database: "testdb",
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded RunQueryInput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.SQL != "SELECT * FROM users" {
		t.Errorf("expected SQL 'SELECT * FROM users', got '%s'", decoded.SQL)
	}
	if decoded.MaxRows == nil || *decoded.MaxRows != 100 {
		t.Error("max_rows not correctly decoded")
	}
}

func TestRunQueryInputJSONOmitEmpty(t *testing.T) {
	input := RunQueryInput{SQL: "SELECT 1"}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Should not contain max_rows or database when empty
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if _, ok := decoded["max_rows"]; ok {
		t.Error("max_rows should be omitted when nil")
	}
	if _, ok := decoded["database"]; ok {
		t.Error("database should be omitted when empty")
	}
}

func TestQueryResultJSON(t *testing.T) {
	result := QueryResult{
		Columns: []string{"id", "name"},
		Rows: [][]interface{}{
			{1, "Alice"},
			{2, "Bob"},
		},
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded QueryResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(decoded.Columns) != 2 {
		t.Errorf("expected 2 columns, got %d", len(decoded.Columns))
	}
	if len(decoded.Rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(decoded.Rows))
	}
}

func TestPingOutputJSON(t *testing.T) {
	output := PingOutput{
		Success:   true,
		LatencyMs: 5,
		Message:   "pong",
	}
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded PingOutput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if !decoded.Success {
		t.Error("expected success to be true")
	}
	if decoded.Message != "pong" {
		t.Errorf("expected message 'pong', got '%s'", decoded.Message)
	}
}

func TestServerInfoOutputJSON(t *testing.T) {
	output := ServerInfoOutput{
		Version:          "8.0.30",
		VersionComment:   "MySQL Community Server",
		Uptime:           12345,
		CurrentDatabase:  "testdb",
		CurrentUser:      "root@localhost",
		CharacterSet:     "utf8mb4",
		Collation:        "utf8mb4_general_ci",
		MaxConnections:   151,
		ThreadsConnected: 5,
	}
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ServerInfoOutput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Version != "8.0.30" {
		t.Errorf("expected version '8.0.30', got '%s'", decoded.Version)
	}
}

// ===== Connection Types JSON Tests =====

func TestConnectionInfoJSON(t *testing.T) {
	info := ConnectionInfo{
		Name:        "production",
		DSN:         "user:***@tcp(localhost:3306)/db",
		Description: "Production database",
		Active:      true,
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ConnectionInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Name != "production" {
		t.Errorf("expected name 'production', got '%s'", decoded.Name)
	}
	if !decoded.Active {
		t.Error("expected active to be true")
	}
}

func TestListConnectionsOutputJSON(t *testing.T) {
	output := ListConnectionsOutput{
		Connections: []ConnectionInfo{
			{Name: "conn1", Active: true},
			{Name: "conn2", Active: false},
		},
		Active: "conn1",
	}
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ListConnectionsOutput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(decoded.Connections) != 2 {
		t.Errorf("expected 2 connections, got %d", len(decoded.Connections))
	}
	if decoded.Active != "conn1" {
		t.Errorf("expected active 'conn1', got '%s'", decoded.Active)
	}
}

func TestUseConnectionInputJSON(t *testing.T) {
	input := UseConnectionInput{Name: "conn2"}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded UseConnectionInput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Name != "conn2" {
		t.Errorf("expected name 'conn2', got '%s'", decoded.Name)
	}
}

func TestUseConnectionOutputJSON(t *testing.T) {
	output := UseConnectionOutput{
		Success:  true,
		Active:   "conn2",
		Message:  "Switched to connection 'conn2'",
		Database: "testdb",
	}
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded UseConnectionOutput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if !decoded.Success {
		t.Error("expected success to be true")
	}
	if decoded.Database != "testdb" {
		t.Errorf("expected database 'testdb', got '%s'", decoded.Database)
	}
}

// ===== Extended Types JSON Tests =====

func TestIndexInfoJSON(t *testing.T) {
	info := IndexInfo{
		Name:      "PRIMARY",
		Columns:   "id",
		NonUnique: false,
		Type:      "BTREE",
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded IndexInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Name != "PRIMARY" {
		t.Errorf("expected name 'PRIMARY', got '%s'", decoded.Name)
	}
	if decoded.NonUnique {
		t.Error("expected non_unique to be false")
	}
}

func TestViewInfoJSON(t *testing.T) {
	info := ViewInfo{
		Name:        "user_view",
		Definer:     "root@localhost",
		Security:    "DEFINER",
		IsUpdatable: "YES",
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ViewInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Name != "user_view" {
		t.Errorf("expected name 'user_view', got '%s'", decoded.Name)
	}
}

func TestTriggerInfoJSON(t *testing.T) {
	info := TriggerInfo{
		Name:      "before_insert_users",
		Event:     "INSERT",
		Table:     "users",
		Timing:    "BEFORE",
		Statement: "SET NEW.created_at = NOW()",
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded TriggerInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Event != "INSERT" {
		t.Errorf("expected event 'INSERT', got '%s'", decoded.Event)
	}
}

func TestProcedureInfoJSON(t *testing.T) {
	info := ProcedureInfo{
		Name:      "get_users",
		Definer:   "root@localhost",
		Created:   "2024-01-01 00:00:00",
		Modified:  "2024-01-01 00:00:00",
		ParamList: "IN user_id INT",
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ProcedureInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Name != "get_users" {
		t.Errorf("expected name 'get_users', got '%s'", decoded.Name)
	}
}

func TestFunctionInfoJSON(t *testing.T) {
	info := FunctionInfo{
		Name:    "calc_total",
		Definer: "root@localhost",
		Returns: "DECIMAL(10,2)",
		Created: "2024-01-01 00:00:00",
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded FunctionInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Returns != "DECIMAL(10,2)" {
		t.Errorf("expected returns 'DECIMAL(10,2)', got '%s'", decoded.Returns)
	}
}

func TestDatabaseSizeInfoJSON(t *testing.T) {
	info := DatabaseSizeInfo{
		Name:    "testdb",
		SizeMB:  100.5,
		DataMB:  80.0,
		IndexMB: 20.5,
		Tables:  25,
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded DatabaseSizeInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.SizeMB != 100.5 {
		t.Errorf("expected size_mb 100.5, got %f", decoded.SizeMB)
	}
}

func TestTableSizeInfoJSON(t *testing.T) {
	info := TableSizeInfo{
		Name:    "users",
		Rows:    1000,
		DataMB:  5.0,
		IndexMB: 1.5,
		TotalMB: 6.5,
		Engine:  "InnoDB",
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded TableSizeInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Rows != 1000 {
		t.Errorf("expected rows 1000, got %d", decoded.Rows)
	}
	if decoded.Engine != "InnoDB" {
		t.Errorf("expected engine 'InnoDB', got '%s'", decoded.Engine)
	}
}

func TestForeignKeyInfoJSON(t *testing.T) {
	info := ForeignKeyInfo{
		Name:             "fk_orders_user",
		Table:            "orders",
		Column:           "user_id",
		ReferencedTable:  "users",
		ReferencedColumn: "id",
		OnUpdate:         "CASCADE",
		OnDelete:         "RESTRICT",
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ForeignKeyInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Name != "fk_orders_user" {
		t.Errorf("expected name 'fk_orders_user', got '%s'", decoded.Name)
	}
	if decoded.OnUpdate != "CASCADE" {
		t.Errorf("expected on_update 'CASCADE', got '%s'", decoded.OnUpdate)
	}
}

func TestPartitionInfoJSON(t *testing.T) {
	info := PartitionInfo{
		Name:        "p0",
		Method:      "RANGE",
		Expression:  "year(created_at)",
		Description: "2024",
		TableRows:   50000,
		DataLength:  1048576,
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded PartitionInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Method != "RANGE" {
		t.Errorf("expected method 'RANGE', got '%s'", decoded.Method)
	}
}

func TestStatusVariableJSON(t *testing.T) {
	v := StatusVariable{Name: "Uptime", Value: "12345"}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded StatusVariable
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Name != "Uptime" {
		t.Errorf("expected name 'Uptime', got '%s'", decoded.Name)
	}
}

func TestServerVariableJSON(t *testing.T) {
	v := ServerVariable{Name: "max_connections", Value: "151"}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ServerVariable
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Name != "max_connections" {
		t.Errorf("expected name 'max_connections', got '%s'", decoded.Name)
	}
}

// ===== Vector Types JSON Tests =====

func TestVectorSearchInputJSON(t *testing.T) {
	input := VectorSearchInput{
		Database:     "testdb",
		Table:        "embeddings",
		Column:       "vector",
		Query:        []float64{0.1, 0.2, 0.3},
		Limit:        10,
		Select:       "id, title",
		Where:        "active = 1",
		DistanceFunc: "cosine",
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded VectorSearchInput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(decoded.Query) != 3 {
		t.Errorf("expected 3 query values, got %d", len(decoded.Query))
	}
	if decoded.DistanceFunc != "cosine" {
		t.Errorf("expected distance_func 'cosine', got '%s'", decoded.DistanceFunc)
	}
}

func TestVectorSearchResultJSON(t *testing.T) {
	result := VectorSearchResult{
		Distance: 0.123,
		Data: map[string]interface{}{
			"id":    1,
			"title": "Test Document",
		},
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded VectorSearchResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Distance != 0.123 {
		t.Errorf("expected distance 0.123, got %f", decoded.Distance)
	}
}

func TestVectorColumnInfoJSON(t *testing.T) {
	info := VectorColumnInfo{
		Table:      "embeddings",
		Column:     "vector",
		Dimensions: 768,
		IndexName:  "idx_vector",
		IndexType:  "HNSW",
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded VectorColumnInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Dimensions != 768 {
		t.Errorf("expected dimensions 768, got %d", decoded.Dimensions)
	}
}

func TestVectorInfoOutputJSON(t *testing.T) {
	output := VectorInfoOutput{
		Columns: []VectorColumnInfo{
			{Table: "docs", Column: "embedding", Dimensions: 768},
		},
		VectorSupport: true,
		MySQLVersion:  "9.0.0",
	}
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded VectorInfoOutput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if !decoded.VectorSupport {
		t.Error("expected vector_support to be true")
	}
	if decoded.MySQLVersion != "9.0.0" {
		t.Errorf("expected mysql_version '9.0.0', got '%s'", decoded.MySQLVersion)
	}
}

// ===== Explain Types JSON Tests =====

func TestExplainQueryInputJSON(t *testing.T) {
	input := ExplainQueryInput{
		SQL:      "SELECT * FROM users",
		Database: "testdb",
		Format:   "json",
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ExplainQueryInput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Format != "json" {
		t.Errorf("expected format 'json', got '%s'", decoded.Format)
	}
}

func TestShowCreateTableOutputJSON(t *testing.T) {
	output := ShowCreateTableOutput{
		CreateStatement: "CREATE TABLE `users` (\n  `id` int NOT NULL AUTO_INCREMENT,\n  PRIMARY KEY (`id`)\n)",
	}
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ShowCreateTableOutput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.CreateStatement == "" {
		t.Error("expected non-empty create_statement")
	}
}

// ===== Empty Input Types Tests =====

func TestEmptyInputTypesJSON(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
	}{
		{"ListDatabasesInput", ListDatabasesInput{}},
		{"PingInput", PingInput{}},
		{"ServerInfoInput", ServerInfoInput{}},
		{"ListConnectionsInput", ListConnectionsInput{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.input)
			if err != nil {
				t.Fatalf("failed to marshal %s: %v", tt.name, err)
			}
			if string(data) != "{}" {
				t.Errorf("expected '{}' for %s, got '%s'", tt.name, string(data))
			}
		})
	}
}
