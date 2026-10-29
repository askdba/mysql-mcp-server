-- SPDX-License-Identifier: Apache-2.0
-- Copyright askdba
--
-- Removes the mysql_mcp SQL tool pack: the whole `mysql_mcp` schema, which holds only
-- the pack's routines. Grants on mysql_mcp.* stay in the grant tables until revoked.
DROP DATABASE IF EXISTS mysql_mcp;
