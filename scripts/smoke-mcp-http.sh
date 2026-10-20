#!/usr/bin/env bash
# End-to-end smoke tests for the MCP Streamable HTTP transport (and REST/stdio regressions),
# run against the real binary and a throwaway MySQL container.
#
# Usage: scripts/smoke-mcp-http.sh [mysql-image-tag] [host-port]
#   mysql-image-tag  official mysql image tag (default: 8.0)
#   host-port        host port for MySQL (default: 13350); the server uses host-port+1000..+1004
# Env:
#   BIN              server binary to test (default: build from this checkout)
#   MYSQL_IMAGE      MySQL image to run (default: mysql:<mysql-image-tag>)
#
# Requires Linux, Docker, curl, jq, openssl, python3. Takes about 1.5 minutes
# (the slow-body check waits for the 30s body read limit).
set -u

TAG=${1:-8.0}
IMAGE=${MYSQL_IMAGE:-mysql:$TAG}
DBPORT=${2:-13350}
APORT=$((DBPORT + 1000))
REPO=$(cd "$(dirname "$0")/.." && pwd)
W=$(mktemp -d)
CNAME=mcp-smoke-$TAG-$$
PIDS=()

cleanup() {
	for p in "${PIDS[@]}"; do kill "$p" 2>/dev/null; done
	docker rm -f "$CNAME" >/dev/null 2>&1
	rm -rf "${W:?}"
}
trap cleanup EXIT

if [ -z "${BIN:-}" ]; then
	BIN=$W/mysql-mcp-server
	(cd "$REPO" && go build -o "$BIN" ./cmd/mysql-mcp-server) || { echo "build failed"; exit 1; }
fi

PASS=0
FAIL=0
ok() { PASS=$((PASS + 1)); printf '  PASS  %s\n' "$1"; }
bad() { FAIL=$((FAIL + 1)); printf '  FAIL  %s  -- %s\n' "$1" "$2"; }
check() { if [ "$2" = "$3" ]; then ok "$1"; else bad "$1" "got '$2', want '$3'"; fi; }
has() { if printf '%s' "$2" | grep -qi -- "$3"; then ok "$1"; else bad "$1" "missing '$3' in: $(printf '%s' "$2" | head -c 300)"; fi; }
between() { awk -v v="$1" -v lo="$2" -v hi="$3" 'BEGIN { exit !(v >= lo && v <= hi) }'; }
now() { date +%s.%N; }
since() { awk -v s="$1" -v e="$(now)" 'BEGIN { printf "%.1f", e - s }'; }

export MYSQL_DSN="mcpuser:mcppass00@tcp(127.0.0.1:$DBPORT)/testdb?parseTime=true"

echo "== MySQL $TAG: starting $IMAGE"
docker run -d --name "$CNAME" -p "127.0.0.1:$DBPORT:3306" -e MYSQL_ROOT_PASSWORD=testpass "$IMAGE" >/dev/null || { echo "docker run failed"; exit 1; }
ready=0
for _ in $(seq 1 120); do
	docker exec "$CNAME" mysql -uroot -ptestpass -e 'select 1' >/dev/null 2>&1 && { ready=1; break; }
	sleep 2
done
[ "$ready" = 1 ] || { echo "MySQL $TAG did not become ready"; exit 1; }
docker exec -i "$CNAME" mysql -uroot -ptestpass <"$REPO/tests/sql/init.sql" 2>/dev/null
docker exec -i "$CNAME" mysql -uroot -ptestpass <"$REPO/tests/sql/mcp_test_user.sql" 2>/dev/null
VER=$(docker exec "$CNAME" mysql -uroot -ptestpass -N -e 'select version()' 2>/dev/null)
echo "   server version: $VER"

start_srv() { # name env...
	local name=$1
	shift
	env "$@" "$BIN" >"$W/$name.log" 2>&1 &
	echo $! >"$W/$name.pid"
	PIDS+=("$!")
}
wait_health() {
	for _ in $(seq 1 60); do curl -sfk "$1" >/dev/null 2>&1 && return 0; sleep 0.25; done
	return 1
}
stop_srv() { # name -> prints seconds until exit
	local pid s
	pid=$(cat "$W/$1.pid")
	s=$(now)
	kill -TERM "$pid" 2>/dev/null
	while kill -0 "$pid" 2>/dev/null; do sleep 0.1; done
	since "$s"
}

# MCP JSON-RPC over HTTP helpers.
mcp_init() { # url [curl args...] -> sets SID
	local url=$1
	shift
	SID=$(curl -sk -D - -o /dev/null -X POST "$url" "$@" -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
		-d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"smoke","version":"0"}}}' |
		grep -i '^mcp-session-id' | awk '{print $2}' | tr -d '\r')
	curl -sk -o /dev/null -X POST "$url" "$@" -H "Mcp-Session-Id: $SID" -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
		-d '{"jsonrpc":"2.0","method":"notifications/initialized"}'
}
mcp_rpc() { # url sid json [curl args...]
	local url=$1 sid=$2 body=$3
	shift 3
	curl -sk -X POST "$url" "$@" ${sid:+-H "Mcp-Session-Id: $sid"} -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' -d "$body" |
		sed -n 's/^data: //p'
}
tool() { # url sid name argsjson [curl args...]
	local url=$1 sid=$2 name=$3 args=$4
	shift 4
	mcp_rpc "$url" "$sid" "{\"jsonrpc\":\"2.0\",\"id\":9,\"method\":\"tools/call\",\"params\":{\"name\":\"$name\",\"arguments\":$args}}" "$@"
}

# Sends a request that promises 100 body bytes, sends 1 and stalls; prints
# "<seconds until the server closed the connection> <status line>".
slow_body() {
	python3 -I - "$1" <<'PY'
import socket, sys, time
s = socket.create_connection(("127.0.0.1", int(sys.argv[1])))
s.sendall(b"POST /mcp HTTP/1.1\r\nHost: 127.0.0.1\r\nContent-Type: application/json\r\n"
          b"Accept: application/json, text/event-stream\r\nContent-Length: 100\r\n\r\n{")
start = time.time()
s.settimeout(60)
data = b""
while True:
    try:
        chunk = s.recv(4096)
    except socket.timeout:
        break
    if not chunk:
        break
    data += chunk
print("%.1f %s" % (time.time() - start, data.split(b"\r\n", 1)[0].decode(errors="replace")))
PY
}

echo "== 1. REST API mode (regression)"
start_srv rest MYSQL_MCP_HTTP=1 MYSQL_MCP_EXTENDED=1 MYSQL_HTTP_PORT=$APORT
if wait_health "http://127.0.0.1:$APORT/health"; then
	for ep in health api api/ping api/databases api/server-info; do
		check "REST GET /$ep" "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$APORT/$ep")" 200
	done
	has "REST /api/databases lists testdb" "$(curl -s "http://127.0.0.1:$APORT/api/databases")" testdb
	has "REST /api/query runs SELECT" "$(curl -s -X POST "http://127.0.0.1:$APORT/api/query" -H 'Content-Type: application/json' -d '{"sql":"SELECT COUNT(*) AS n FROM users","database":"testdb"}')" '"n"'
	t=$(stop_srv rest)
	has "REST graceful shutdown (${t}s)" "$(cat "$W/rest.log")" "Server stopped gracefully"
else bad "REST server start" "$(tail -3 "$W/rest.log")"; fi

echo "== 2. stdio MCP (regression)"
STDIO=$({
	printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"s","version":"0"}}}' \
		'{"jsonrpc":"2.0","method":"notifications/initialized"}' \
		'{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
		'{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"run_query","arguments":{"sql":"SELECT name FROM users ORDER BY id LIMIT 1","database":"testdb"}}}'
	sleep 2
} | MYSQL_MCP_EXTENDED=1 timeout 15 "$BIN" 2>/dev/null)
STDIO_TOOLS=$(printf '%s\n' "$STDIO" | jq -rs '[.[] | select(.id==2) | .result.tools[].name] | sort | join(",")')
has "stdio tools/list" "$STDIO_TOOLS" run_query
has "stdio run_query returns rows" "$(printf '%s\n' "$STDIO" | jq -c 'select(.id==3) | .result.structuredContent')" '"rows"'

echo "== 3. MCP Streamable HTTP, loopback, no token"
HP=$((APORT + 1))
U=http://127.0.0.1:$HP/mcp
start_srv http MYSQL_MCP_TRANSPORT=http MYSQL_MCP_EXTENDED=1 MYSQL_HTTP_PORT=$HP MYSQL_MCP_TOKEN_TRACKING=1
if wait_health "http://127.0.0.1:$HP/health"; then
	mcp_init "$U"
	if [ -n "$SID" ]; then ok "initialize returns Mcp-Session-Id"; else bad "initialize" "no session id"; fi
	HTTP_TOOLS=$(mcp_rpc "$U" "$SID" '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | jq -r '[.result.tools[].name] | sort | join(",")')
	check "HTTP tools/list == stdio tools/list" "$HTTP_TOOLS" "$STDIO_TOOLS"
	has "list_databases" "$(tool "$U" "$SID" list_databases '{}')" testdb
	has "describe_table users" "$(tool "$U" "$SID" describe_table '{"database":"testdb","table":"users"}')" '"columns"'
	has "run_query SELECT" "$(tool "$U" "$SID" run_query '{"sql":"SELECT id, name FROM users ORDER BY id","database":"testdb"}')" '"rows"'
	has "run_query rejects DELETE" "$(tool "$U" "$SID" run_query '{"sql":"DELETE FROM users","database":"testdb"}')" '"isError":true'
	has "run_query rejects multi-statement" "$(tool "$U" "$SID" run_query '{"sql":"SELECT 1; DROP TABLE users","database":"testdb"}')" '"isError":true'
	has "list_connections" "$(tool "$U" "$SID" list_connections '{}')" '"active"'
	check "GET /health" "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$HP/health")" 200
	has "GET /api/metrics/tokens" "$(curl -s "http://127.0.0.1:$HP/api/metrics/tokens")" run_query
	check "DNS-rebinding Host rejected" "$(curl -s -o /dev/null -w '%{http_code}' -X POST "$U" -H 'Host: evil.example' -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' -d '{}')" 403
	check "cross-origin POST rejected" "$(curl -s -o /dev/null -w '%{http_code}' -X POST "$U" -H 'Origin: https://evil.example' -H 'Sec-Fetch-Site: cross-site' -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' -d '{}')" 403
	check "sub-path /mcp/x not routed" "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$HP/mcp/x")" 404
	SLOW=$(slow_body "$HP")
	el=${SLOW%% *}
	if between "$el" 25 40 && printf '%s' "$SLOW" | grep -q " 400 "; then
		ok "slow body: 400 and connection closed after ${el}s (limit 30s)"
	else bad "slow body cutoff" "$SLOW"; fi
	(curl -s -N "$U" -H "Mcp-Session-Id: $SID" -H 'Accept: text/event-stream' >/dev/null &)
	sleep 1
	t=$(stop_srv http)
	if between "$t" 0 8; then ok "shutdown with open stream in ${t}s (grace 5s)"; else bad "shutdown with open stream" "${t}s"; fi
else bad "HTTP server start" "$(tail -3 "$W/http.log")"; fi

echo "== 4. Remote bind: token + TLS + CORS"
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -keyout "$W/key.pem" -out "$W/cert.pem" -days 1 -subj /CN=localhost >/dev/null 2>&1
TOKEN=smoke-$$-$RANDOM
printf '%s' "$TOKEN" >"$W/token"
TP=$((APORT + 2))
TU=https://127.0.0.1:$TP/mcp
start_srv tls MYSQL_MCP_TRANSPORT=http MYSQL_HTTP_PORT=$TP MYSQL_MCP_HTTP_BIND=0.0.0.0 MYSQL_MCP_HTTP_TOKEN_FILE="$W/token" \
	MYSQL_MCP_HTTP_TLS_CERT="$W/cert.pem" MYSQL_MCP_HTTP_TLS_KEY="$W/key.pem" MYSQL_MCP_HTTP_ALLOWED_ORIGINS=https://app.example
if wait_health "https://127.0.0.1:$TP/health"; then
	INIT='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"s","version":"0"}}}'
	check "HTTPS without token -> 401" "$(curl -sk -o /dev/null -w '%{http_code}' -X POST "$TU" -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' -d "$INIT")" 401
	check "HTTPS wrong token -> 401" "$(curl -sk -o /dev/null -w '%{http_code}' -X POST "$TU" -H 'Authorization: Bearer nope' -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' -d "$INIT")" 401
	mcp_init "$TU" -H "Authorization: Bearer $TOKEN"
	has "HTTPS + token: run_query" "$(tool "$TU" "$SID" run_query '{"sql":"SELECT COUNT(*) AS n FROM products","database":"testdb"}' -H "Authorization: Bearer $TOKEN")" '"rows"'
	check "plain HTTP to TLS port rejected" "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$TP/health")" 400
	PRE=$(curl -sk -D - -o /dev/null -X OPTIONS "$TU" -H 'Origin: https://app.example' -H 'Access-Control-Request-Method: POST' -H 'Access-Control-Request-Headers: authorization, content-type')
	has "CORS preflight (allowed origin, before auth) -> 204" "$PRE" " 204"
	has "CORS allow-origin header" "$PRE" "Access-Control-Allow-Origin: https://app.example"
	check "/api/metrics/tokens needs token" "$(curl -sk -o /dev/null -w '%{http_code}' "https://127.0.0.1:$TP/api/metrics/tokens")" 401
	stop_srv tls >/dev/null
else bad "TLS server start" "$(tail -3 "$W/tls.log")"; fi

echo "== 5. Unsafe configurations refused at startup"
refuse() { # label expected env...
	local label=$1 want=$2 out
	shift 2
	out=$(env "$@" timeout 10 "$BIN" 2>&1 | tail -1)
	has "refuses: $label" "$out" "$want"
}
refuse "remote bind without token" "bearer token is required" MYSQL_MCP_TRANSPORT=http MYSQL_MCP_HTTP_BIND=0.0.0.0
refuse "remote bind without TLS" "TLS is required" MYSQL_MCP_TRANSPORT=http MYSQL_MCP_HTTP_BIND=0.0.0.0 MYSQL_MCP_HTTP_TOKEN=x
refuse "REST + HTTP transport" "cannot be combined" MYSQL_MCP_TRANSPORT=http MYSQL_MCP_HTTP=1
refuse "path /health" "reserved" MYSQL_MCP_TRANSPORT=http MYSQL_MCP_HTTP_PATH=/health
refuse "bind with port" "without a port" MYSQL_MCP_TRANSPORT=http MYSQL_MCP_HTTP_BIND=0.0.0.0:9306
refuse "unknown transport" "invalid MYSQL_MCP_TRANSPORT" MYSQL_MCP_TRANSPORT=sse
printf 'connections:\n  default:\n    dsn: "%s"\ntransport: http\nhttp:\n  mcp:\n    bind: 0.0.0.0\n' "$MYSQL_DSN" >"$W/bad.yaml"
has "--validate-config catches remote bind without token" "$(env -u MYSQL_DSN "$BIN" --validate-config "$W/bad.yaml" 2>&1)" "bearer token is required"

echo "== 6. Stateless mode"
SP=$((APORT + 3))
SU=http://127.0.0.1:$SP/mcp
start_srv stateless MYSQL_MCP_TRANSPORT=http MYSQL_MCP_HTTP_STATELESS=1 MYSQL_MCP_EXTENDED=1 MYSQL_MCP_ENABLE_ADD_CONNECTION=1 MYSQL_HTTP_PORT=$SP
if wait_health "http://127.0.0.1:$SP/health"; then
	has "stateless tools/call without initialize" "$(tool "$SU" "" list_databases '{}')" testdb
	has "stateless add_connection refused" "$(tool "$SU" "" add_connection "{\"name\":\"x\",\"dsn\":\"$MYSQL_DSN\"}")" "stateless"
	stop_srv stateless >/dev/null
else bad "stateless server start" "$(tail -3 "$W/stateless.log")"; fi

echo "== 7. add_connection is scoped to the session"
AP=$((APORT + 4))
AU=http://127.0.0.1:$AP/mcp
start_srv addconn MYSQL_MCP_TRANSPORT=http MYSQL_MCP_EXTENDED=1 MYSQL_MCP_ENABLE_ADD_CONNECTION=1 MYSQL_HTTP_PORT=$AP
if wait_health "http://127.0.0.1:$AP/health"; then
	mcp_init "$AU"
	S1=$SID
	mcp_init "$AU"
	S2=$SID
	has "session 1 add_connection extra" "$(tool "$AU" "$S1" add_connection "{\"name\":\"extra\",\"dsn\":\"$MYSQL_DSN\"}")" '"success":true'
	has "session 1 sees extra as active" "$(tool "$AU" "$S1" list_connections '{}')" '"active":"extra"'
	L2=$(tool "$AU" "$S2" list_connections '{}')
	if printf '%s' "$L2" | grep -q '"name":"extra"\|mcp-session/'; then bad "session 2 cannot see extra" "$L2"; else ok "session 2 cannot see extra"; fi
	has "session 2 cannot use_connection extra" "$(tool "$AU" "$S2" use_connection '{"name":"extra"}')" "not found"
	has "session 2 may reuse the name" "$(tool "$AU" "$S2" add_connection "{\"name\":\"extra\",\"dsn\":\"$MYSQL_DSN\"}")" '"success":true'
	curl -s -o /dev/null -X DELETE "$AU" -H "Mcp-Session-Id: $S1"
	sleep 1
	has "ending session 1 closes its connection" "$(cat "$W/addconn.log")" "closed session connection"
	stop_srv addconn >/dev/null
else bad "addconn server start" "$(tail -3 "$W/addconn.log")"; fi

echo "== MySQL $TAG ($VER): $PASS passed, $FAIL failed"
[ "$FAIL" = 0 ]
