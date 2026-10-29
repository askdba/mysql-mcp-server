-- Dedicated MCP / integration-test user (not root).
-- Run as MySQL root after testdb exists (see init.sql).
-- Credentials: mcpuser / mcppass00

SET NAMES utf8mb4;

CREATE USER IF NOT EXISTS 'mcpuser'@'localhost' IDENTIFIED BY 'mcppass00';
CREATE USER IF NOT EXISTS 'mcpuser'@'%' IDENTIFIED BY 'mcppass00';

-- testdb: integration tests (mcp_tools_test, mariadb_*) create tables as mcpuser; needs DDL + DML.
-- sakila: remain read-only via mcp_test_user_sakila.sql only.
GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, DROP, ALTER, INDEX, REFERENCES, CREATE TEMPORARY TABLES, SHOW VIEW, EXECUTE ON `testdb`.* TO 'mcpuser'@'localhost';
GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, DROP, ALTER, INDEX, REFERENCES, CREATE TEMPORARY TABLES, SHOW VIEW, EXECUTE ON `testdb`.* TO 'mcpuser'@'%';

-- mysql_mcp: the SQL tool pack (install-schema). Least privilege for installing and
-- upgrading it, plus EXECUTE to call it (cmd/mysql-mcp-server/toolpack_integration_test.go).
GRANT CREATE, DROP, CREATE ROUTINE, ALTER ROUTINE, EXECUTE ON `mysql_mcp`.* TO 'mcpuser'@'localhost';
GRANT CREATE, DROP, CREATE ROUTINE, ALTER ROUTINE, EXECUTE ON `mysql_mcp`.* TO 'mcpuser'@'%';

FLUSH PRIVILEGES;
