-- SPDX-License-Identifier: Apache-2.0
-- Copyright askdba
--
-- mysql_mcp SQL tool pack, version 1.0.0 (RFC 0001 §5.1, askdba/mysql-mcp-server).
--
-- Installs the schema `mysql_mcp` with one stored procedure per catalog tool of
-- mysql-mcp-server. Each procedure returns exactly the result set of the query that the
-- Go server runs when the pack is not installed: same columns, same order, same rows.
-- The Go server keeps the logic around those queries (input checks, access control,
-- row limits, error messages); the pack only moves the SQL into the server.
--
-- Install or upgrade (re-runnable; every routine is dropped and recreated):
--   mysql -u <admin> -p < mysql_mcp.sql
--   or: mysql-mcp-server install-schema
-- The installing account needs CREATE, DROP, CREATE ROUTINE and ALTER ROUTINE on
-- mysql_mcp.* (no SUPER: the pack has no stored functions).
-- Grant to an MCP account:
--   GRANT EXECUTE ON mysql_mcp.* TO 'mcp'@'%';
--
-- Rules for every routine:
--   * SQL SECURITY INVOKER: a caller sees exactly what its own grants allow.
--   * Each query is a fixed statement text, run as a prepared statement with its
--     arguments bound to `?`, the way the Go driver runs it. Comparing a routine
--     parameter directly would use the parameter's collation: case-insensitive matching
--     of names on MySQL, or "Illegal mix of collations" on MariaDB 11.5+, where
--     information_schema uses utf8mb3_uca1400_ai_ci. A bound `?` takes the column's
--     collation instead, as for the Go server, on every supported server.
--   * Arguments are passed through the session variables @mysql_mcp_1..3 (MySQL's
--     EXECUTE ... USING accepts only user variables); they are reset to NULL afterwards,
--     also on error. The prepared statement is named mysql_mcp_stmt.
--   * No routine accepts SQL. Names are checked against information_schema before a
--     dynamic SHOW (SHOW INDEX, SHOW CREATE TABLE) is built, and they are quoted.
--   * NULL means "not given" for optional arguments.
--
-- Supported: MySQL 8.0, 8.4, 9.7, 26.10; MariaDB 10.6+ (see README.md).

CREATE DATABASE IF NOT EXISTS mysql_mcp DEFAULT CHARACTER SET utf8mb4;

DROP FUNCTION IF EXISTS mysql_mcp.version;
DROP PROCEDURE IF EXISTS mysql_mcp.version;
DROP PROCEDURE IF EXISTS mysql_mcp.list_databases;
DROP PROCEDURE IF EXISTS mysql_mcp.list_tables;
DROP PROCEDURE IF EXISTS mysql_mcp.describe_table;
DROP PROCEDURE IF EXISTS mysql_mcp.show_create_table;
DROP PROCEDURE IF EXISTS mysql_mcp.list_indexes;
DROP PROCEDURE IF EXISTS mysql_mcp.list_views;
DROP PROCEDURE IF EXISTS mysql_mcp.list_triggers;
DROP PROCEDURE IF EXISTS mysql_mcp.list_procedures;
DROP PROCEDURE IF EXISTS mysql_mcp.list_functions;
DROP PROCEDURE IF EXISTS mysql_mcp.list_partitions;
DROP PROCEDURE IF EXISTS mysql_mcp.database_size;
DROP PROCEDURE IF EXISTS mysql_mcp.table_size;
DROP PROCEDURE IF EXISTS mysql_mcp.foreign_keys;
DROP PROCEDURE IF EXISTS mysql_mcp.search_tables;
DROP PROCEDURE IF EXISTS mysql_mcp.search_columns;
DROP PROCEDURE IF EXISTS mysql_mcp.list_status;
DROP PROCEDURE IF EXISTS mysql_mcp.list_variables;
DROP PROCEDURE IF EXISTS mysql_mcp.process_list;
DROP PROCEDURE IF EXISTS mysql_mcp.vector_columns;
DROP PROCEDURE IF EXISTS mysql_mcp.vector_column_index;
DROP PROCEDURE IF EXISTS mysql_mcp.require_table;

DELIMITER $$

-- Pack version (semantic versioning): CALL mysql_mcp.version() returns one row, `version`.
-- Callers use the pack only when the major version is one they support; a new major
-- version may change result sets. A procedure rather than a function: with binary
-- logging on (the MySQL default), CREATE FUNCTION needs SUPER or
-- log_bin_trust_function_creators, which least-privilege and managed-service installers
-- do not have.
CREATE PROCEDURE mysql_mcp.version()
    NO SQL SQL SECURITY INVOKER
    COMMENT 'mysql_mcp tool pack version'
BEGIN
    SELECT '1.0.0' AS version;
END$$

-- Fails with error 1146 (ER_NO_SUCH_TABLE) unless p_database.p_table is visible to the
-- caller in information_schema.TABLES. Guards the dynamic SHOW statements.
CREATE PROCEDURE mysql_mcp.require_table(
    IN p_database VARCHAR(64),
    IN p_table VARCHAR(64))
    READS SQL DATA SQL SECURITY INVOKER
    COMMENT 'Internal: fail unless the table exists and is visible'
BEGIN
    DECLARE v_msg VARCHAR(128);
    DECLARE EXIT HANDLER FOR SQLEXCEPTION
    BEGIN
        SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
        RESIGNAL;
    END;
    SET @mysql_mcp_3 = 0;
    IF p_database IS NOT NULL AND p_table IS NOT NULL THEN
        SET @mysql_mcp_1 = p_database;
        SET @mysql_mcp_2 = p_table;
        PREPARE mysql_mcp_stmt FROM 'SELECT COUNT(*) INTO @mysql_mcp_3 FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?';
        EXECUTE mysql_mcp_stmt USING @mysql_mcp_1, @mysql_mcp_2;
        DEALLOCATE PREPARE mysql_mcp_stmt;
    END IF;
    IF @mysql_mcp_3 = 0 THEN
        SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
        SET v_msg = LEFT(CONCAT('Table ''', IFNULL(p_database, ''), '.', IFNULL(p_table, ''), ''' doesn''t exist'), 128);
        SIGNAL SQLSTATE '42S02' SET MYSQL_ERRNO = 1146, MESSAGE_TEXT = v_msg;
    END IF;
    SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
END$$

-- list_databases
CREATE PROCEDURE mysql_mcp.list_databases()
    READS SQL DATA SQL SECURITY INVOKER
    COMMENT 'mysql-mcp-server tool list_databases'
BEGIN
    DECLARE EXIT HANDLER FOR SQLEXCEPTION
    BEGIN
        SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
        RESIGNAL;
    END;
    PREPARE mysql_mcp_stmt FROM 'SELECT SCHEMA_NAME FROM information_schema.SCHEMATA ORDER BY SCHEMA_NAME, CAST(SCHEMA_NAME AS BINARY)';
    EXECUTE mysql_mcp_stmt;
    DEALLOCATE PREPARE mysql_mcp_stmt;
    SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
END$$

-- list_tables
CREATE PROCEDURE mysql_mcp.list_tables(
    IN p_database VARCHAR(64))
    READS SQL DATA SQL SECURITY INVOKER
    COMMENT 'mysql-mcp-server tool list_tables'
BEGIN
    DECLARE EXIT HANDLER FOR SQLEXCEPTION
    BEGIN
        SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
        RESIGNAL;
    END;
    SET @mysql_mcp_1 = p_database;
    PREPARE mysql_mcp_stmt FROM 'SELECT TABLE_NAME, ENGINE, TABLE_ROWS, TABLE_COMMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME, CAST(TABLE_NAME AS BINARY)';
    EXECUTE mysql_mcp_stmt USING @mysql_mcp_1;
    DEALLOCATE PREPARE mysql_mcp_stmt;
    SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
END$$

-- describe_table
CREATE PROCEDURE mysql_mcp.describe_table(
    IN p_database VARCHAR(64),
    IN p_table VARCHAR(64))
    READS SQL DATA SQL SECURITY INVOKER
    COMMENT 'mysql-mcp-server tool describe_table'
BEGIN
    DECLARE EXIT HANDLER FOR SQLEXCEPTION
    BEGIN
        SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
        RESIGNAL;
    END;
    SET @mysql_mcp_1 = p_database;
    SET @mysql_mcp_2 = p_table;
    PREPARE mysql_mcp_stmt FROM 'SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_KEY, COLUMN_DEFAULT, EXTRA, COLUMN_COMMENT, COLLATION_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION';
    EXECUTE mysql_mcp_stmt USING @mysql_mcp_1, @mysql_mcp_2;
    DEALLOCATE PREPARE mysql_mcp_stmt;
    SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
END$$

-- show_create_table: SHOW CREATE TABLE `db`.`table`
CREATE PROCEDURE mysql_mcp.show_create_table(
    IN p_database VARCHAR(64),
    IN p_table VARCHAR(64))
    READS SQL DATA SQL SECURITY INVOKER
    COMMENT 'mysql-mcp-server tool show_create_table'
BEGIN
    DECLARE EXIT HANDLER FOR SQLEXCEPTION
    BEGIN
        SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
        RESIGNAL;
    END;
    CALL mysql_mcp.require_table(p_database, p_table);
    SET @mysql_mcp_1 = CONCAT('SHOW CREATE TABLE `', REPLACE(p_database, '`', '``'), '`.`', REPLACE(p_table, '`', '``'), '`');
    PREPARE mysql_mcp_stmt FROM @mysql_mcp_1;
    SET @mysql_mcp_1 = NULL;
    EXECUTE mysql_mcp_stmt;
    DEALLOCATE PREPARE mysql_mcp_stmt;
END$$

-- list_indexes: SHOW INDEX FROM `db`.`table`
CREATE PROCEDURE mysql_mcp.list_indexes(
    IN p_database VARCHAR(64),
    IN p_table VARCHAR(64))
    READS SQL DATA SQL SECURITY INVOKER
    COMMENT 'mysql-mcp-server tool list_indexes'
BEGIN
    DECLARE EXIT HANDLER FOR SQLEXCEPTION
    BEGIN
        SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
        RESIGNAL;
    END;
    CALL mysql_mcp.require_table(p_database, p_table);
    SET @mysql_mcp_1 = CONCAT('SHOW INDEX FROM `', REPLACE(p_database, '`', '``'), '`.`', REPLACE(p_table, '`', '``'), '`');
    PREPARE mysql_mcp_stmt FROM @mysql_mcp_1;
    SET @mysql_mcp_1 = NULL;
    EXECUTE mysql_mcp_stmt;
    DEALLOCATE PREPARE mysql_mcp_stmt;
END$$

-- list_views
CREATE PROCEDURE mysql_mcp.list_views(
    IN p_database VARCHAR(64))
    READS SQL DATA SQL SECURITY INVOKER
    COMMENT 'mysql-mcp-server tool list_views'
BEGIN
    DECLARE EXIT HANDLER FOR SQLEXCEPTION
    BEGIN
        SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
        RESIGNAL;
    END;
    SET @mysql_mcp_1 = p_database;
    PREPARE mysql_mcp_stmt FROM 'SELECT TABLE_NAME, DEFINER, SECURITY_TYPE, IS_UPDATABLE FROM information_schema.VIEWS WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME, CAST(TABLE_NAME AS BINARY)';
    EXECUTE mysql_mcp_stmt USING @mysql_mcp_1;
    DEALLOCATE PREPARE mysql_mcp_stmt;
    SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
END$$

-- list_triggers
CREATE PROCEDURE mysql_mcp.list_triggers(
    IN p_database VARCHAR(64))
    READS SQL DATA SQL SECURITY INVOKER
    COMMENT 'mysql-mcp-server tool list_triggers'
BEGIN
    DECLARE EXIT HANDLER FOR SQLEXCEPTION
    BEGIN
        SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
        RESIGNAL;
    END;
    SET @mysql_mcp_1 = p_database;
    PREPARE mysql_mcp_stmt FROM 'SELECT TRIGGER_NAME, EVENT_MANIPULATION, EVENT_OBJECT_TABLE, ACTION_TIMING, LEFT(ACTION_STATEMENT, 200) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA = ? ORDER BY TRIGGER_NAME';
    EXECUTE mysql_mcp_stmt USING @mysql_mcp_1;
    DEALLOCATE PREPARE mysql_mcp_stmt;
    SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
END$$

-- list_procedures
CREATE PROCEDURE mysql_mcp.list_procedures(
    IN p_database VARCHAR(64))
    READS SQL DATA SQL SECURITY INVOKER
    COMMENT 'mysql-mcp-server tool list_procedures'
BEGIN
    DECLARE EXIT HANDLER FOR SQLEXCEPTION
    BEGIN
        SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
        RESIGNAL;
    END;
    SET @mysql_mcp_1 = p_database;
    PREPARE mysql_mcp_stmt FROM 'SELECT ROUTINE_NAME, DEFINER, CREATED, LAST_ALTERED, IFNULL(PARAMETER_STYLE, '''') FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA = ? AND ROUTINE_TYPE = ''PROCEDURE'' ORDER BY ROUTINE_NAME';
    EXECUTE mysql_mcp_stmt USING @mysql_mcp_1;
    DEALLOCATE PREPARE mysql_mcp_stmt;
    SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
END$$

-- list_functions
CREATE PROCEDURE mysql_mcp.list_functions(
    IN p_database VARCHAR(64))
    READS SQL DATA SQL SECURITY INVOKER
    COMMENT 'mysql-mcp-server tool list_functions'
BEGIN
    DECLARE EXIT HANDLER FOR SQLEXCEPTION
    BEGIN
        SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
        RESIGNAL;
    END;
    SET @mysql_mcp_1 = p_database;
    PREPARE mysql_mcp_stmt FROM 'SELECT ROUTINE_NAME, DEFINER, DTD_IDENTIFIER, CREATED FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA = ? AND ROUTINE_TYPE = ''FUNCTION'' ORDER BY ROUTINE_NAME';
    EXECUTE mysql_mcp_stmt USING @mysql_mcp_1;
    DEALLOCATE PREPARE mysql_mcp_stmt;
    SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
END$$

-- list_partitions
CREATE PROCEDURE mysql_mcp.list_partitions(
    IN p_database VARCHAR(64),
    IN p_table VARCHAR(64))
    READS SQL DATA SQL SECURITY INVOKER
    COMMENT 'mysql-mcp-server tool list_partitions'
BEGIN
    DECLARE EXIT HANDLER FOR SQLEXCEPTION
    BEGIN
        SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
        RESIGNAL;
    END;
    SET @mysql_mcp_1 = p_database;
    SET @mysql_mcp_2 = p_table;
    PREPARE mysql_mcp_stmt FROM 'SELECT PARTITION_NAME, PARTITION_METHOD, PARTITION_EXPRESSION, PARTITION_DESCRIPTION, TABLE_ROWS, DATA_LENGTH FROM information_schema.PARTITIONS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND PARTITION_NAME IS NOT NULL ORDER BY PARTITION_ORDINAL_POSITION, SUBPARTITION_ORDINAL_POSITION';
    EXECUTE mysql_mcp_stmt USING @mysql_mcp_1, @mysql_mcp_2;
    DEALLOCATE PREPARE mysql_mcp_stmt;
    SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
END$$

-- database_size: one database, or (NULL) every database except the system schemas
CREATE PROCEDURE mysql_mcp.database_size(
    IN p_database VARCHAR(64))
    READS SQL DATA SQL SECURITY INVOKER
    COMMENT 'mysql-mcp-server tool database_size'
BEGIN
    DECLARE EXIT HANDLER FOR SQLEXCEPTION
    BEGIN
        SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
        RESIGNAL;
    END;
    IF p_database IS NOT NULL THEN
        SET @mysql_mcp_1 = p_database;
        PREPARE mysql_mcp_stmt FROM 'SELECT TABLE_SCHEMA, ROUND(SUM(DATA_LENGTH + INDEX_LENGTH) / 1024 / 1024, 2) as size_mb, ROUND(SUM(DATA_LENGTH) / 1024 / 1024, 2) as data_mb, ROUND(SUM(INDEX_LENGTH) / 1024 / 1024, 2) as index_mb, COUNT(*) as tables FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? GROUP BY TABLE_SCHEMA ORDER BY size_mb DESC, TABLE_SCHEMA, CAST(TABLE_SCHEMA AS BINARY)';
        EXECUTE mysql_mcp_stmt USING @mysql_mcp_1;
        DEALLOCATE PREPARE mysql_mcp_stmt;
    ELSE
        PREPARE mysql_mcp_stmt FROM 'SELECT TABLE_SCHEMA, ROUND(SUM(DATA_LENGTH + INDEX_LENGTH) / 1024 / 1024, 2) as size_mb, ROUND(SUM(DATA_LENGTH) / 1024 / 1024, 2) as data_mb, ROUND(SUM(INDEX_LENGTH) / 1024 / 1024, 2) as index_mb, COUNT(*) as tables FROM information_schema.TABLES WHERE TABLE_SCHEMA NOT IN (''information_schema'', ''performance_schema'', ''mysql'', ''sys'') GROUP BY TABLE_SCHEMA ORDER BY size_mb DESC, TABLE_SCHEMA, CAST(TABLE_SCHEMA AS BINARY)';
        EXECUTE mysql_mcp_stmt;
        DEALLOCATE PREPARE mysql_mcp_stmt;
    END IF;
    SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
END$$

-- table_size: every table of a database, or (p_table not NULL) one table
CREATE PROCEDURE mysql_mcp.table_size(
    IN p_database VARCHAR(64),
    IN p_table VARCHAR(64))
    READS SQL DATA SQL SECURITY INVOKER
    COMMENT 'mysql-mcp-server tool table_size'
BEGIN
    DECLARE EXIT HANDLER FOR SQLEXCEPTION
    BEGIN
        SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
        RESIGNAL;
    END;
    IF p_table IS NOT NULL THEN
        SET @mysql_mcp_1 = p_database;
        SET @mysql_mcp_2 = p_table;
        PREPARE mysql_mcp_stmt FROM 'SELECT TABLE_NAME, TABLE_ROWS, ROUND(DATA_LENGTH / 1024 / 1024, 2) as data_mb, ROUND(INDEX_LENGTH / 1024 / 1024, 2) as index_mb, ROUND((DATA_LENGTH + INDEX_LENGTH) / 1024 / 1024, 2) as total_mb, ENGINE FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY total_mb DESC, TABLE_NAME';
        EXECUTE mysql_mcp_stmt USING @mysql_mcp_1, @mysql_mcp_2;
        DEALLOCATE PREPARE mysql_mcp_stmt;
    ELSE
        SET @mysql_mcp_1 = p_database;
        PREPARE mysql_mcp_stmt FROM 'SELECT TABLE_NAME, TABLE_ROWS, ROUND(DATA_LENGTH / 1024 / 1024, 2) as data_mb, ROUND(INDEX_LENGTH / 1024 / 1024, 2) as index_mb, ROUND((DATA_LENGTH + INDEX_LENGTH) / 1024 / 1024, 2) as total_mb, ENGINE FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? ORDER BY total_mb DESC, TABLE_NAME';
        EXECUTE mysql_mcp_stmt USING @mysql_mcp_1;
        DEALLOCATE PREPARE mysql_mcp_stmt;
    END IF;
    SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
END$$

-- foreign_keys: every foreign key of a database, or (p_table not NULL) of one table
CREATE PROCEDURE mysql_mcp.foreign_keys(
    IN p_database VARCHAR(64),
    IN p_table VARCHAR(64))
    READS SQL DATA SQL SECURITY INVOKER
    COMMENT 'mysql-mcp-server tool foreign_keys'
BEGIN
    DECLARE EXIT HANDLER FOR SQLEXCEPTION
    BEGIN
        SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
        RESIGNAL;
    END;
    IF p_table IS NOT NULL THEN
        SET @mysql_mcp_1 = p_database;
        SET @mysql_mcp_2 = p_table;
        PREPARE mysql_mcp_stmt FROM 'SELECT CONSTRAINT_NAME, TABLE_NAME, COLUMN_NAME, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME, (SELECT UPDATE_RULE FROM information_schema.REFERENTIAL_CONSTRAINTS rc WHERE rc.CONSTRAINT_SCHEMA = kcu.CONSTRAINT_SCHEMA AND rc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME) as on_update, (SELECT DELETE_RULE FROM information_schema.REFERENTIAL_CONSTRAINTS rc WHERE rc.CONSTRAINT_SCHEMA = kcu.CONSTRAINT_SCHEMA AND rc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME) as on_delete FROM information_schema.KEY_COLUMN_USAGE kcu WHERE CONSTRAINT_SCHEMA = ? AND REFERENCED_TABLE_NAME IS NOT NULL AND TABLE_NAME = ? ORDER BY TABLE_NAME, CONSTRAINT_NAME, ORDINAL_POSITION';
        EXECUTE mysql_mcp_stmt USING @mysql_mcp_1, @mysql_mcp_2;
        DEALLOCATE PREPARE mysql_mcp_stmt;
    ELSE
        SET @mysql_mcp_1 = p_database;
        PREPARE mysql_mcp_stmt FROM 'SELECT CONSTRAINT_NAME, TABLE_NAME, COLUMN_NAME, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME, (SELECT UPDATE_RULE FROM information_schema.REFERENTIAL_CONSTRAINTS rc WHERE rc.CONSTRAINT_SCHEMA = kcu.CONSTRAINT_SCHEMA AND rc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME) as on_update, (SELECT DELETE_RULE FROM information_schema.REFERENTIAL_CONSTRAINTS rc WHERE rc.CONSTRAINT_SCHEMA = kcu.CONSTRAINT_SCHEMA AND rc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME) as on_delete FROM information_schema.KEY_COLUMN_USAGE kcu WHERE CONSTRAINT_SCHEMA = ? AND REFERENCED_TABLE_NAME IS NOT NULL ORDER BY TABLE_NAME, CONSTRAINT_NAME, ORDINAL_POSITION';
        EXECUTE mysql_mcp_stmt USING @mysql_mcp_1;
        DEALLOCATE PREPARE mysql_mcp_stmt;
    END IF;
    SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
END$$

-- search_schema (tables): names LIKE p_pattern in one database, or (NULL) in every database except the system schemas; at most p_limit rows
CREATE PROCEDURE mysql_mcp.search_tables(
    IN p_pattern VARCHAR(255),
    IN p_database VARCHAR(64),
    IN p_limit INT UNSIGNED)
    READS SQL DATA SQL SECURITY INVOKER
    COMMENT 'mysql-mcp-server tool search_schema (tables)'
BEGIN
    DECLARE EXIT HANDLER FOR SQLEXCEPTION
    BEGIN
        SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
        RESIGNAL;
    END;
    IF p_database IS NOT NULL THEN
        SET @mysql_mcp_1 = p_pattern;
        SET @mysql_mcp_2 = p_database;
        SET @mysql_mcp_3 = p_limit;
        PREPARE mysql_mcp_stmt FROM 'SELECT TABLE_SCHEMA, TABLE_NAME FROM information_schema.TABLES WHERE TABLE_NAME LIKE ? AND TABLE_SCHEMA = ? ORDER BY TABLE_SCHEMA, CAST(TABLE_SCHEMA AS BINARY), TABLE_NAME, CAST(TABLE_NAME AS BINARY) LIMIT ?';
        EXECUTE mysql_mcp_stmt USING @mysql_mcp_1, @mysql_mcp_2, @mysql_mcp_3;
        DEALLOCATE PREPARE mysql_mcp_stmt;
    ELSE
        SET @mysql_mcp_1 = p_pattern;
        SET @mysql_mcp_2 = p_limit;
        PREPARE mysql_mcp_stmt FROM 'SELECT TABLE_SCHEMA, TABLE_NAME FROM information_schema.TABLES WHERE TABLE_NAME LIKE ? AND TABLE_SCHEMA NOT IN (''information_schema'', ''performance_schema'', ''mysql'', ''sys'') ORDER BY TABLE_SCHEMA, CAST(TABLE_SCHEMA AS BINARY), TABLE_NAME, CAST(TABLE_NAME AS BINARY) LIMIT ?';
        EXECUTE mysql_mcp_stmt USING @mysql_mcp_1, @mysql_mcp_2;
        DEALLOCATE PREPARE mysql_mcp_stmt;
    END IF;
    SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
END$$

-- search_schema (columns): names LIKE p_pattern in one database, or (NULL) in every database except the system schemas; at most p_limit rows
CREATE PROCEDURE mysql_mcp.search_columns(
    IN p_pattern VARCHAR(255),
    IN p_database VARCHAR(64),
    IN p_limit INT UNSIGNED)
    READS SQL DATA SQL SECURITY INVOKER
    COMMENT 'mysql-mcp-server tool search_schema (columns)'
BEGIN
    DECLARE EXIT HANDLER FOR SQLEXCEPTION
    BEGIN
        SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
        RESIGNAL;
    END;
    IF p_database IS NOT NULL THEN
        SET @mysql_mcp_1 = p_pattern;
        SET @mysql_mcp_2 = p_database;
        SET @mysql_mcp_3 = p_limit;
        PREPARE mysql_mcp_stmt FROM 'SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME FROM information_schema.COLUMNS WHERE COLUMN_NAME LIKE ? AND TABLE_SCHEMA = ? ORDER BY TABLE_SCHEMA, CAST(TABLE_SCHEMA AS BINARY), TABLE_NAME, CAST(TABLE_NAME AS BINARY), ORDINAL_POSITION LIMIT ?';
        EXECUTE mysql_mcp_stmt USING @mysql_mcp_1, @mysql_mcp_2, @mysql_mcp_3;
        DEALLOCATE PREPARE mysql_mcp_stmt;
    ELSE
        SET @mysql_mcp_1 = p_pattern;
        SET @mysql_mcp_2 = p_limit;
        PREPARE mysql_mcp_stmt FROM 'SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME FROM information_schema.COLUMNS WHERE COLUMN_NAME LIKE ? AND TABLE_SCHEMA NOT IN (''information_schema'', ''performance_schema'', ''mysql'', ''sys'') ORDER BY TABLE_SCHEMA, CAST(TABLE_SCHEMA AS BINARY), TABLE_NAME, CAST(TABLE_NAME AS BINARY), ORDINAL_POSITION LIMIT ?';
        EXECUTE mysql_mcp_stmt USING @mysql_mcp_1, @mysql_mcp_2;
        DEALLOCATE PREPARE mysql_mcp_stmt;
    END IF;
    SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
END$$

-- list_status: performance_schema.global_status, optionally LIKE p_pattern. Fails where that table is unavailable; callers then use SHOW GLOBAL STATUS, as the Go server does without the pack
CREATE PROCEDURE mysql_mcp.list_status(
    IN p_pattern VARCHAR(255))
    READS SQL DATA SQL SECURITY INVOKER
    COMMENT 'mysql-mcp-server tool list_status'
BEGIN
    DECLARE EXIT HANDLER FOR SQLEXCEPTION
    BEGIN
        SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
        RESIGNAL;
    END;
    IF p_pattern IS NOT NULL THEN
        SET @mysql_mcp_1 = p_pattern;
        PREPARE mysql_mcp_stmt FROM 'SELECT VARIABLE_NAME, VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME LIKE ? ORDER BY VARIABLE_NAME';
        EXECUTE mysql_mcp_stmt USING @mysql_mcp_1;
        DEALLOCATE PREPARE mysql_mcp_stmt;
    ELSE
        PREPARE mysql_mcp_stmt FROM 'SELECT VARIABLE_NAME, VARIABLE_VALUE FROM performance_schema.global_status ORDER BY VARIABLE_NAME';
        EXECUTE mysql_mcp_stmt;
        DEALLOCATE PREPARE mysql_mcp_stmt;
    END IF;
    SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
END$$

-- list_variables: SHOW GLOBAL VARIABLES, optionally WHERE Variable_name LIKE p_pattern (the
-- Go server's first choice; some managed deployments stall on
-- performance_schema.global_variables). SHOW ... LIKE ? cannot be prepared; the WHERE form can.
CREATE PROCEDURE mysql_mcp.list_variables(
    IN p_pattern VARCHAR(255))
    READS SQL DATA SQL SECURITY INVOKER
    COMMENT 'mysql-mcp-server tool list_variables'
BEGIN
    DECLARE EXIT HANDLER FOR SQLEXCEPTION
    BEGIN
        SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
        RESIGNAL;
    END;
    IF p_pattern IS NOT NULL THEN
        SET @mysql_mcp_1 = p_pattern;
        PREPARE mysql_mcp_stmt FROM 'SHOW GLOBAL VARIABLES WHERE Variable_name LIKE ?';
        EXECUTE mysql_mcp_stmt USING @mysql_mcp_1;
        DEALLOCATE PREPARE mysql_mcp_stmt;
    ELSE
        PREPARE mysql_mcp_stmt FROM 'SHOW GLOBAL VARIABLES';
        EXECUTE mysql_mcp_stmt;
        DEALLOCATE PREPARE mysql_mcp_stmt;
    END IF;
    SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
END$$

-- process_list: SHOW FULL PROCESSLIST (other users' threads need the PROCESS privilege)
CREATE PROCEDURE mysql_mcp.process_list()
    READS SQL DATA SQL SECURITY INVOKER
    COMMENT 'mysql-mcp-server tool process_list'
BEGIN
    DECLARE EXIT HANDLER FOR SQLEXCEPTION
    BEGIN
        SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
        RESIGNAL;
    END;
    PREPARE mysql_mcp_stmt FROM 'SHOW FULL PROCESSLIST';
    EXECUTE mysql_mcp_stmt;
    DEALLOCATE PREPARE mysql_mcp_stmt;
    SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
END$$

-- vector_info (columns): VECTOR columns of a database, or (p_table not NULL) of one table
CREATE PROCEDURE mysql_mcp.vector_columns(
    IN p_database VARCHAR(64),
    IN p_table VARCHAR(64))
    READS SQL DATA SQL SECURITY INVOKER
    COMMENT 'mysql-mcp-server tool vector_info (columns)'
BEGIN
    DECLARE EXIT HANDLER FOR SQLEXCEPTION
    BEGIN
        SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
        RESIGNAL;
    END;
    IF p_table IS NOT NULL THEN
        SET @mysql_mcp_1 = p_database;
        SET @mysql_mcp_2 = p_table;
        PREPARE mysql_mcp_stmt FROM 'SELECT TABLE_NAME, COLUMN_NAME, COLUMN_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND COLUMN_TYPE LIKE ''vector%'' AND TABLE_NAME = ? ORDER BY TABLE_NAME, ORDINAL_POSITION';
        EXECUTE mysql_mcp_stmt USING @mysql_mcp_1, @mysql_mcp_2;
        DEALLOCATE PREPARE mysql_mcp_stmt;
    ELSE
        SET @mysql_mcp_1 = p_database;
        PREPARE mysql_mcp_stmt FROM 'SELECT TABLE_NAME, COLUMN_NAME, COLUMN_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND COLUMN_TYPE LIKE ''vector%'' ORDER BY TABLE_NAME, ORDINAL_POSITION';
        EXECUTE mysql_mcp_stmt USING @mysql_mcp_1;
        DEALLOCATE PREPARE mysql_mcp_stmt;
    END IF;
    SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
END$$

-- vector_info (index): the first index (by name) on one column, or no row
CREATE PROCEDURE mysql_mcp.vector_column_index(
    IN p_database VARCHAR(64),
    IN p_table VARCHAR(64),
    IN p_column VARCHAR(64))
    READS SQL DATA SQL SECURITY INVOKER
    COMMENT 'mysql-mcp-server tool vector_info (index)'
BEGIN
    DECLARE EXIT HANDLER FOR SQLEXCEPTION
    BEGIN
        SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
        RESIGNAL;
    END;
    SET @mysql_mcp_1 = p_database;
    SET @mysql_mcp_2 = p_table;
    SET @mysql_mcp_3 = p_column;
    PREPARE mysql_mcp_stmt FROM 'SELECT INDEX_NAME, INDEX_TYPE FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND COLUMN_NAME = ? ORDER BY INDEX_NAME LIMIT 1';
    EXECUTE mysql_mcp_stmt USING @mysql_mcp_1, @mysql_mcp_2, @mysql_mcp_3;
    DEALLOCATE PREPARE mysql_mcp_stmt;
    SET @mysql_mcp_1 = NULL, @mysql_mcp_2 = NULL, @mysql_mcp_3 = NULL;
END$$

DELIMITER ;
