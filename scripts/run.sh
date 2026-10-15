#!/usr/bin/env bash
#
# Starts the local stack and prints every link, credential, connection string,
# a ready-to-use JWT and copy-pasteable curl examples.
#
#   scripts/run.sh [up]      build, start, wait until healthy, print info (default)
#   scripts/run.sh info      print info for the running stack
#   scripts/run.sh stop      stop the stack (data volumes are kept)
#
# Host ports are chosen automatically: an explicitly exported *_PORT variable
# wins, a running stack keeps its ports, otherwise the default is used, or
# default+10000 and upwards when it is taken by something else.
#
# Environment knobs: NO_BUILD=1 (skip image build), WAIT_TIMEOUT=seconds
# (default 240), TOKEN_SUB (default dev-user), TOKEN_TTL seconds (default 86400),
# NO_COLOR=1.
#
# Works with the stock macOS bash 3.2.

set -euo pipefail
cd "$(dirname "$0")/.."

readonly TOPIC=companies.events.v1
readonly SERVICE_NAME=companies-service

# service  container-port  env-var  default-host-port
readonly PORTS=(
	"api            8080 API_PORT        8080"
	"postgres       5432 POSTGRES_PORT   5432"
	"redpanda       19092 REDPANDA_PORT  19092"
	"grafana        3000 GRAFANA_PORT    3000"
	"prometheus     9090 PROMETHEUS_PORT 9090"
	"tempo          3200 TEMPO_PORT      3200"
	"loki           3100 LOKI_PORT       3100"
	"otel-collector 4317 OTLP_GRPC_PORT  4317"
	"otel-collector 4318 OTLP_HTTP_PORT  4318"
	"redpanda-console 8080 CONSOLE_PORT 8081"
	"pgadmin        80   PGADMIN_PORT    5050"
)

# --- output helpers ----------------------------------------------------------

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
	B=$'\033[1m' DIM=$'\033[2m' RED=$'\033[31m' GREEN=$'\033[32m' YELLOW=$'\033[33m' CYAN=$'\033[36m' R=$'\033[0m'
else
	B='' DIM='' RED='' GREEN='' YELLOW='' CYAN='' R=''
fi

say() { printf '%s\n' "$*"; }
step() { printf '%s==>%s %s\n' "$CYAN$B" "$R" "$*"; }
warn() { printf '%s!%s %s\n' "$YELLOW$B" "$R" "$*" >&2; }
die() {
	printf '%serror:%s %s\n' "$RED$B" "$R" "$*" >&2
	exit 1
}
section() { printf '\n%s%s%s\n' "$B$CYAN" "$1" "$R"; }
kv() { printf '  %-16s %s\n' "$1" "$2"; }
cmd() { printf '  %s%s%s\n' "$DIM" "$1" "$R"; }

# --- prerequisites -----------------------------------------------------------

require() {
	local tool
	for tool in docker curl openssl; do
		command -v "$tool" >/dev/null 2>&1 || die "'$tool' is required but not installed"
	done
	docker compose version >/dev/null 2>&1 || die "Docker Compose v2 ('docker compose') is required"
	docker info >/dev/null 2>&1 || die "the Docker daemon is not reachable; is Docker running?"
}

# --- ports -------------------------------------------------------------------

port_busy() { (: <"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }

# running_port SERVICE CONTAINER_PORT: the published host port, if running.
running_port() {
	# Fails when the service is not running; that just means "no port yet".
	{ docker compose port "$1" "$2" 2>/dev/null || true; } |
		sed -n 's/.*:\([0-9][0-9]*\)$/\1/p' | head -n 1
}

# free_port DEFAULT TAKEN: first usable port, trying DEFAULT, DEFAULT+10000, ...
free_port() {
	local p=$1 taken=$2
	if ! port_busy "$p" && [[ $taken != *" $p "* ]]; then
		echo "$p"
		return
	fi
	p=$(($1 + 10000))
	[ "$p" -gt 65000 ] && p=20000
	while port_busy "$p" || [[ $taken == *" $p "* ]]; do
		p=$((p + 1))
	done
	echo "$p"
}

# resolve_ports: exports every *_PORT variable used by docker-compose.yml.
resolve_ports() {
	local taken=" " line svc cport var def port moved=""
	for line in "${PORTS[@]}"; do
		read -r svc cport var def <<<"$line"
		port=${!var:-}
		[ -z "$port" ] && port=$(running_port "$svc" "$cport")
		if [ -z "$port" ]; then
			port=$(free_port "$def" "$taken")
			[ "$port" != "$def" ] && moved="$moved $var=$port"
		fi
		taken="$taken$port "
		printf -v "$var" '%s' "$port"
		export "${var?}"
	done
	if [ -n "$moved" ]; then
		warn "default ports in use, remapped:$moved"
	fi
}

# --- configuration values ----------------------------------------------------

# container_env NAME: value of NAME inside the running api container.
container_env() {
	local id
	id=$(docker compose ps -q api 2>/dev/null | head -n 1)
	[ -n "$id" ] || return 0
	docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$id" |
		sed -n "s/^$1=//p" | head -n 1
}

# file_env NAME: value from .env.example, overridden by .env.
file_env() {
	local f line v=""
	for f in .env.example .env; do
		[ -f "$f" ] || continue
		line=$(grep -E "^$1=" "$f" | tail -n 1) && v=${line#*=}
	done
	v=${v%\"} v=${v#\"} v=${v%\'} v=${v#\'}
	printf '%s' "$v"
}

config_value() {
	local v
	v=$(container_env "$1")
	[ -n "$v" ] || v=$(file_env "$1")
	printf '%s' "$v"
}

# --- JWT (HS256) -------------------------------------------------------------

b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }

make_token() {
	local secret iss aud sub now exp claims header payload sig
	secret=$(config_value COMPANIES_AUTH_JWT_SECRET)
	[ -n "$secret" ] || die "COMPANIES_AUTH_JWT_SECRET is not set (see .env.example)"
	iss=$(config_value COMPANIES_AUTH_ISSUER)
	aud=$(config_value COMPANIES_AUTH_AUDIENCE)
	sub=${TOKEN_SUB:-dev-user}
	now=$(date +%s)
	exp=$((now + ${TOKEN_TTL:-86400}))
	TOKEN_EXPIRES=$exp

	claims="\"sub\":\"$sub\",\"iat\":$now,\"nbf\":$now,\"exp\":$exp"
	[ -n "$iss" ] && claims="$claims,\"iss\":\"$iss\""
	[ -n "$aud" ] && claims="$claims,\"aud\":\"$aud\""

	header=$(printf '%s' '{"alg":"HS256","typ":"JWT"}' | b64url)
	payload=$(printf '{%s}' "$claims" | b64url)
	sig=$(printf '%s.%s' "$header" "$payload" | openssl dgst -sha256 -hmac "$secret" -binary | b64url)
	TOKEN="$header.$payload.$sig"
}

# token_status: checks the token against the API without changing anything
# (deleting a random id answers 404 when authenticated, 401 otherwise).
token_status() {
	local code
	code=$(curl -s -o /dev/null -w '%{http_code}' -X DELETE \
		-H "Authorization: Bearer $TOKEN" \
		"http://localhost:$API_PORT/v1/companies/00000000-0000-0000-0000-000000000000" || true)
	case $code in
	404) printf '%saccepted by the API%s' "$GREEN" "$R" ;;
	401) printf '%srejected by the API (secret mismatch?)%s' "$RED" "$R" ;;
	*) printf '%scould not be checked (HTTP %s)%s' "$YELLOW" "$code" "$R" ;;
	esac
}

# --- Grafana / Prometheus deep links -----------------------------------------

urlencode() {
	local s=$1 out="" c i
	for ((i = 0; i < ${#s}; i++)); do
		c=${s:i:1}
		case $c in
		[a-zA-Z0-9.~_-]) out="$out$c" ;;
		*) out="$out$(printf '%%%02X' "'$c")" ;;
		esac
	done
	printf '%s' "$out"
}

explore_url() { # datasource-uid datasource-type query-json-fragment
	local panes
	panes="{\"a\":{\"datasource\":\"$1\",\"queries\":[{\"refId\":\"A\",\"datasource\":{\"type\":\"$2\",\"uid\":\"$1\"},$3}],\"range\":{\"from\":\"now-1h\",\"to\":\"now\"}}}"
	printf 'http://localhost:%s/explore?schemaVersion=1&orgId=1&panes=%s' "$GRAFANA_PORT" "$(urlencode "$panes")"
}

# --- Postman -----------------------------------------------------------------

readonly POSTMAN_ENV=postman/local.postman_environment.json

json_escape() { printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'; }

# write_postman_env generates an environment for the running stack: actual
# ports and the JWT settings the service uses.
write_postman_env() {
	local secret iss aud
	secret=$(json_escape "$(config_value COMPANIES_AUTH_JWT_SECRET)")
	iss=$(json_escape "$(config_value COMPANIES_AUTH_ISSUER)")
	aud=$(json_escape "$(config_value COMPANIES_AUTH_AUDIENCE)")
	cat >"$POSTMAN_ENV" <<JSON
{
  "id": "6f1b3c2e-companies-local-generated",
  "name": "Companies (local stack, generated by make start)",
  "values": [
    {"key": "baseUrl", "value": "http://localhost:$API_PORT", "enabled": true},
    {"key": "grafanaUrl", "value": "http://localhost:$GRAFANA_PORT", "enabled": true},
    {"key": "consoleUrl", "value": "http://localhost:$CONSOLE_PORT", "enabled": true},
    {"key": "pgadminUrl", "value": "http://localhost:$PGADMIN_PORT", "enabled": true},
    {"key": "jwtSecret", "value": "$secret", "type": "secret", "enabled": true},
    {"key": "jwtIssuer", "value": "$iss", "enabled": true},
    {"key": "jwtAudience", "value": "$aud", "enabled": true},
    {"key": "subject", "value": "postman", "enabled": true}
  ],
  "_postman_variable_scope": "environment"
}
JSON
}

# --- readiness ---------------------------------------------------------------

wait_for() { # name url deadline
	local name=$1 url=$2 deadline=$3 start=$SECONDS
	while :; do
		if curl -fsS -o /dev/null --max-time 3 "$url" 2>/dev/null; then
			printf '  %s✓%s %-11s %s(%ss)%s\n' "$GREEN" "$R" "$name" "$DIM" "$((SECONDS - start))" "$R"
			return 0
		fi
		if [ "$SECONDS" -ge "$deadline" ]; then
			printf '  %s✗%s %-11s not ready; see: docker compose logs %s\n' "$RED" "$R" "$name" "$4"
			return 1
		fi
		sleep 2
	done
}

wait_all() {
	local deadline=$((SECONDS + ${WAIT_TIMEOUT:-240})) failed=0
	step "Waiting for services (timeout ${WAIT_TIMEOUT:-240}s)"
	wait_for api "http://localhost:$API_PORT/readyz" "$deadline" api || failed=1
	wait_for grafana "http://localhost:$GRAFANA_PORT/api/health" "$deadline" grafana || failed=1
	wait_for prometheus "http://localhost:$PROMETHEUS_PORT/-/ready" "$deadline" prometheus || failed=1
	wait_for loki "http://localhost:$LOKI_PORT/ready" "$deadline" loki || failed=1
	wait_for tempo "http://localhost:$TEMPO_PORT/ready" "$deadline" tempo || failed=1
	wait_for redpanda-console "http://localhost:$CONSOLE_PORT/admin/health" "$deadline" redpanda-console || failed=1
	wait_for pgadmin "http://localhost:$PGADMIN_PORT/misc/ping" "$deadline" pgadmin || failed=1
	return "$failed"
}

# --- info --------------------------------------------------------------------

print_info() {
	local api="http://localhost:$API_PORT" expires
	make_token
	expires=$(date -r "$TOKEN_EXPIRES" '+%Y-%m-%d %H:%M' 2>/dev/null || date -d "@$TOKEN_EXPIRES" '+%Y-%m-%d %H:%M')

	printf '\n%sCompanies service: local environment%s\n' "$B" "$R"

	section "API"
	kv "Base URL" "$api/v1/companies"
	kv "Liveness" "$api/livez     (process works; never depends on PostgreSQL/Kafka)"
	kv "Readiness" "$api/readyz    (postgres is critical; kafka only degrades; details need a token)"
	kv "Startup" "$api/startupz"
	kv "Metrics" "$api/metrics"
	kv "Swagger UI" "$api/docs/index.html"
	kv "Swagger spec" "$api/docs/doc.json  (generated by swag into api/swagger.yaml)"
	kv "JWT subject" "${TOKEN_SUB:-dev-user}, expires $expires, $(token_status)"
	say ""
	cmd "export TOKEN=$TOKEN"
	say "  In Swagger UI click Authorize and paste:"
	cmd "Bearer $TOKEN"
	say ""
	say "  Try it (paste in order):"
	cmd "ID=\$(curl -s $api/v1/companies -H \"Authorization: Bearer \$TOKEN\" -H 'Content-Type: application/json' \\"
	cmd "  -d '{\"name\":\"Acme-'\$RANDOM'\",\"description\":\"Makes everything\",\"amount_of_employees\":42,\"registered\":true,\"type\":\"CORPORATIONS\"}' \\"
	cmd "  | sed -E 's/.*\"id\":\"([^\"]+)\".*/\\1/'); echo \$ID"
	cmd "curl -s $api/v1/companies/\$ID"
	cmd "curl -s -X PATCH $api/v1/companies/\$ID -H \"Authorization: Bearer \$TOKEN\" \\"
	cmd "  -H 'Content-Type: application/merge-patch+json' -d '{\"description\":null,\"amount_of_employees\":50}'"
	cmd "curl -si -X DELETE $api/v1/companies/\$ID -H \"Authorization: Bearer \$TOKEN\""

	section "Grafana (no login, anonymous admin)"
	kv "Dashboard" "http://localhost:$GRAFANA_PORT/d/companies-service/companies-service"
	kv "Logs (Loki)" "$(explore_url loki loki "\"expr\":\"{service_name=\\\"$SERVICE_NAME\\\"}\"")"
	kv "Traces (Tempo)" "$(explore_url tempo tempo "\"queryType\":\"traceql\",\"query\":\"{resource.service.name=\\\"$SERVICE_NAME\\\"}\",\"limit\":20,\"tableType\":\"traces\"")"
	kv "Service graph" "$(explore_url tempo tempo "\"queryType\":\"serviceMap\",\"serviceMapQuery\":\"{}\"")"
	kv "Event delivery" "$(explore_url tempo tempo "\"queryType\":\"traceql\",\"query\":\"{span.messaging.system=\\\"kafka\\\"}\",\"limit\":20,\"tableType\":\"traces\"")"

	section "Prometheus"
	kv "UI" "http://localhost:$PROMETHEUS_PORT"
	kv "Requests/s" "http://localhost:$PROMETHEUS_PORT/query?g0.expr=$(urlencode 'sum by (route, code) (rate(companies_http_request_duration_seconds_count[1m]))')&g0.tab=graph&g0.range_input=1h"
	kv "Targets" "http://localhost:$PROMETHEUS_PORT/targets"

	section "Redpanda Console (no login; protobuf values decoded from proto/)"
	kv "Overview" "http://localhost:$CONSOLE_PORT/overview"
	kv "Events topic" "http://localhost:$CONSOLE_PORT/topics/$TOPIC"
	kv "Dead letters" "http://localhost:$CONSOLE_PORT/topics/$TOPIC.dlq"

	section "pgAdmin (no login; server \"companies\" connects without a password)"
	kv "UI" "http://localhost:$PGADMIN_PORT/browser/"
	kv "Account" "admin@example.com / admin (only needed if server mode is enabled)"

	section "Postman"
	kv "Collection" "postman/companies.postman_collection.json"
	kv "Environment" "$POSTMAN_ENV  (ports and JWT secret of this stack)"
	kv "Run headless" "make postman  (newman, all folders with assertions)"
	say "  Import both, select the environment, run the collection; folder 5 links to the request's trace."

	section "PostgreSQL"
	kv "Host DSN" "postgres://companies:companies@localhost:$POSTGRES_PORT/companies?sslmode=disable"
	kv "In-network DSN" "postgres://companies:companies@postgres:5432/companies?sslmode=disable"
	kv "User/password/db" "companies / companies / companies"
	kv "psql" "docker compose exec postgres psql -U companies"

	section "Redpanda (Kafka API)"
	kv "Bootstrap (host)" "localhost:$REDPANDA_PORT"
	kv "Bootstrap (net)" "redpanda:9092"
	kv "Topic" "$TOPIC (key = company id, value = protobuf companies.events.v1.CompanyEvent)"
	kv "Contract" "proto/companies/events/v1/events.proto"
	kv "rpk" "docker compose exec redpanda rpk topic consume $TOPIC -n 5"
	kv "Dead letters" "$TOPIC.dlq (original payload + dlq_* headers)"
	kv "Tail events" "make events"

	section "Telemetry endpoints"
	kv "OTLP gRPC" "localhost:$OTLP_GRPC_PORT"
	kv "OTLP HTTP" "http://localhost:$OTLP_HTTP_PORT  (POST /v1/traces, /v1/logs)"
	kv "Tempo API" "http://localhost:$TEMPO_PORT/api/search?tags=service.name%3D$SERVICE_NAME"
	kv "Loki API" "http://localhost:$LOKI_PORT/loki/api/v1/labels"

	section "Commands"
	kv "make info" "print this again"
	kv "make stop" "stop everything (data is kept)"
	kv "make clean" "stop and delete all data volumes"
	kv "make events" "tail the Kafka topic"
	kv "make dlq" "tail the dead-letter topic"
	kv "make postman" "run the Postman collection against the stack"
	kv "logs" "docker compose logs -f api"
	say ""
}

stack_running() { [ -n "$(docker compose ps -q --status running api 2>/dev/null)" ]; }

# --- commands ----------------------------------------------------------------

cmd_up() {
	require
	resolve_ports
	if [ -n "${NO_BUILD:-}" ]; then
		step "Starting the stack (no build)"
		docker compose up -d --no-build --remove-orphans
	else
		step "Building and starting the stack"
		docker compose up -d --build --remove-orphans
	fi
	local ready=0
	wait_all || ready=1
	write_postman_env
	print_info
	if [ "$ready" -ne 0 ]; then
		warn "some services did not become ready in time; the links above may not all work yet"
		return 1
	fi
}

cmd_info() {
	require
	stack_running || die "the stack is not running; start it with 'make start'"
	resolve_ports
	write_postman_env
	print_info
}

cmd_stop() {
	require
	step "Stopping the stack (volumes are kept; 'make clean' removes them)"
	docker compose down --remove-orphans
}

case ${1:-up} in
up | start) cmd_up ;;
info) cmd_info ;;
stop | down) cmd_stop ;;
-h | --help | help) sed -n '2,19p' "$0" | sed 's/^# \{0,1\}//' ;;
*) die "unknown command '$1' (expected up, info or stop)" ;;
esac
