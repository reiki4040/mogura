#!/usr/bin/env bash
#
# end to end test for mogura with the local test bastion.
#
# it launches the containers of local-env/docker-compose.yml, then checks that
# mogura tunnels to the nginx container, and that host key verification accepts
# and rejects the connection as expected.
#
# usage: make test-e2e
#
# `set -e` is not used on purpose. every scenario runs even if an earlier one
# fails, so one run reports every problem.
set -uo pipefail

cd "$(dirname "$0")/.."

readonly MOGURA=bin/mogura
readonly COMPOSE_FILE=local-env/docker-compose.yml
readonly SAMPLE_CONFIG=config.yml.sample
readonly KNOWN_HOSTS=local-env/known_hosts

readonly BASTION_HOST=localhost
readonly BASTION_PORT=2222
# the port config.yml.sample binds locally.
readonly LOCAL_PORT=8080

readonly SSHD_WAIT_COUNT=60
readonly TUNNEL_WAIT_COUNT=15
readonly MOGURA_EXIT_WAIT_COUNT=15

TMPDIR_E2E=""
MOGURA_PID=""
compose_started=""
failures=0
scenario=""

log() {
	printf '%s\n' "$*"
}

pass() {
	printf 'PASS %s\n\n' "$scenario"
}

fail() {
	printf 'FAIL %s: %s\n\n' "$scenario" "$*"
	failures=$((failures + 1))
}

begin() {
	scenario="$1"
	printf -- '--- %s\n' "$scenario"
}

stop_mogura() {
	if [ -n "$MOGURA_PID" ]; then
		kill "$MOGURA_PID" 2>/dev/null
		wait "$MOGURA_PID" 2>/dev/null
		MOGURA_PID=""
	fi
}

cleanup() {
	local status=$?

	stop_mogura

	if [ -n "$compose_started" ]; then
		log "--- tearing down containers"
		docker compose -f "$COMPOSE_FILE" down >/dev/null 2>&1
	fi

	if [ -n "$TMPDIR_E2E" ] && [ -d "$TMPDIR_E2E" ]; then
		rm -rf "$TMPDIR_E2E"
	fi

	return $status
}
trap cleanup EXIT

abort() {
	log "ABORT: $*"
	exit 1
}

# start_mogura launches mogura in the background. its output goes to the given
# log file, so a scenario can check what it reported.
start_mogura() {
	local log_file="$1"
	shift

	"$MOGURA" "$@" >"$log_file" 2>&1 &
	MOGURA_PID=$!
}

# wait_for_sshd waits until the bastion answers with a host key.
wait_for_sshd() {
	local i
	for i in $(seq "$SSHD_WAIT_COUNT"); do
		if [ -n "$(ssh-keyscan -p "$BASTION_PORT" "$BASTION_HOST" 2>/dev/null)" ]; then
			return 0
		fi
		sleep 1
	done

	return 1
}

# http_code requests the nginx container through the tunnel and prints the
# status code. it retries while mogura is still setting the tunnel up.
http_code() {
	local i code
	for i in $(seq "$TUNNEL_WAIT_COUNT"); do
		code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "http://localhost:${LOCAL_PORT}/" 2>/dev/null)
		if [ "$code" = "200" ]; then
			printf '%s' "$code"
			return 0
		fi
		sleep 1
	done

	printf '%s' "${code:-none}"
	return 1
}

# wait_for_mogura_exit waits for mogura to give up, as it must when it can not
# verify the host key.
wait_for_mogura_exit() {
	local i
	for i in $(seq "$MOGURA_EXIT_WAIT_COUNT"); do
		if ! kill -0 "$MOGURA_PID" 2>/dev/null; then
			wait "$MOGURA_PID" 2>/dev/null
			MOGURA_PID=""
			return 0
		fi
		sleep 1
	done

	return 1
}

# config_with rewrites the sample config so that scenarios share one source of
# truth for the tunnel settings.
config_with() {
	local replacement="$1"
	local out="$2"

	sed "s|known_hosts_path: ./local-env/known_hosts|${replacement}|" "$SAMPLE_CONFIG" >"$out"
}

record_host_key() {
	local out="$1"
	shift

	ssh-keyscan "$@" -p "$BASTION_PORT" -H "$BASTION_HOST" >"$out" 2>/dev/null
}

# assert_log_contains checks that mogura reported the given text.
assert_log_contains() {
	local log_file="$1"
	local want="$2"

	if grep -q -- "$want" "$log_file"; then
		return 0
	fi

	log "mogura output:"
	sed 's/^/  /' "$log_file"
	return 1
}

check_prerequisites() {
	command -v docker >/dev/null 2>&1 || abort "docker is not installed."
	docker info >/dev/null 2>&1 || abort "can not connect to the docker daemon. is it running?"
	command -v ssh-keyscan >/dev/null 2>&1 || abort "ssh-keyscan is not installed."
	command -v ssh-keygen >/dev/null 2>&1 || abort "ssh-keygen is not installed."
	command -v curl >/dev/null 2>&1 || abort "curl is not installed."
	[ -x "$MOGURA" ] || abort "$MOGURA does not exist. run make native-build."

	# mogura can not bind the local port if something else already holds it.
	# anything that accepts a connection is in the way, not only an http server.
	if (exec 3<>"/dev/tcp/127.0.0.1/${LOCAL_PORT}") 2>/dev/null; then
		abort "port ${LOCAL_PORT} is already in use. stop it before running the e2e test."
	fi
}

#
# scenarios
#

# the sample config keeps host key verification on, so this also covers the
# workflow that LOCAL_EXAMPLE.md documents.
scenario_verified_tunnel() {
	begin "tunnel works with host key verification"

	if ! record_host_key "$KNOWN_HOSTS"; then
		fail "could not record the bastion host key"
		return
	fi

	local log_file="$TMPDIR_E2E/verified.log"
	start_mogura "$log_file" -config "$SAMPLE_CONFIG"

	local code
	code=$(http_code)
	if [ "$code" != "200" ]; then
		log "mogura output:"
		sed 's/^/  /' "$log_file"
		fail "want http 200 through the tunnel, got $code"
	else
		pass
	fi

	stop_mogura
}

scenario_unknown_host() {
	begin "unknown host is rejected"

	local known_hosts="$TMPDIR_E2E/empty_known_hosts"
	: >"$known_hosts"

	local config="$TMPDIR_E2E/unknown_host.yml"
	config_with "known_hosts_path: $known_hosts" "$config"

	local log_file="$TMPDIR_E2E/unknown_host.log"
	start_mogura "$log_file" -config "$config"

	if ! wait_for_mogura_exit; then
		fail "want mogura to stop, it is still running"
		stop_mogura
		return
	fi

	if ! assert_log_contains "$log_file" "ssh-keyscan"; then
		fail "want the ssh-keyscan hint in the output"
		return
	fi

	pass
}

scenario_changed_host_key() {
	begin "changed host key is rejected"

	# record a key the bastion does not have, as a man in the middle would look.
	local impostor="$TMPDIR_E2E/impostor_key"
	if ! ssh-keygen -t ed25519 -N '' -C impostor -f "$impostor" -q; then
		fail "could not generate the impostor key"
		return
	fi

	local known_hosts="$TMPDIR_E2E/impostor_known_hosts"
	printf '[%s]:%s %s\n' "$BASTION_HOST" "$BASTION_PORT" "$(cut -d' ' -f1,2 "${impostor}.pub")" >"$known_hosts"

	local config="$TMPDIR_E2E/changed_host_key.yml"
	config_with "known_hosts_path: $known_hosts" "$config"

	local log_file="$TMPDIR_E2E/changed_host_key.log"
	start_mogura "$log_file" -config "$config"

	if ! wait_for_mogura_exit; then
		fail "want mogura to stop, it is still running"
		stop_mogura
		return
	fi

	if ! assert_log_contains "$log_file" "man-in-the-middle"; then
		fail "want the output to report a possible attack"
		return
	fi

	if ! assert_log_contains "$log_file" "ssh-keygen"; then
		fail "want the ssh-keygen hint in the output"
		return
	fi

	pass
}

scenario_insecure_option() {
	begin "-insecure-ignore-host-key connects without a recorded key"

	local known_hosts="$TMPDIR_E2E/empty_known_hosts"
	: >"$known_hosts"

	local config="$TMPDIR_E2E/insecure_option.yml"
	config_with "known_hosts_path: $known_hosts" "$config"

	local log_file="$TMPDIR_E2E/insecure_option.log"
	start_mogura "$log_file" -config "$config" -insecure-ignore-host-key

	local code
	code=$(http_code)
	if [ "$code" != "200" ]; then
		log "mogura output:"
		sed 's/^/  /' "$log_file"
		fail "want http 200 through the tunnel, got $code"
		stop_mogura
		return
	fi

	if ! assert_log_contains "$log_file" "WARN host key verification is disabled"; then
		fail "want a warning that verification is disabled"
		stop_mogura
		return
	fi

	pass
	stop_mogura
}

scenario_insecure_config() {
	begin "insecure_ignore_host_key in the config file connects"

	local config="$TMPDIR_E2E/insecure_config.yml"
	config_with "insecure_ignore_host_key: true" "$config"

	local log_file="$TMPDIR_E2E/insecure_config.log"
	start_mogura "$log_file" -config "$config"

	local code
	code=$(http_code)
	if [ "$code" != "200" ]; then
		log "mogura output:"
		sed 's/^/  /' "$log_file"
		fail "want http 200 through the tunnel, got $code"
		stop_mogura
		return
	fi

	if ! assert_log_contains "$log_file" "WARN host key verification is disabled"; then
		fail "want a warning that verification is disabled"
		stop_mogura
		return
	fi

	pass
	stop_mogura
}

# the bastion serves ecdsa, rsa and ed25519 host keys, while OpenSSH records
# only the ed25519 one. the ssh package default prefers rsa-sha2-*, so without
# the algorithms taken from known_hosts this connection fails as a key mismatch.
scenario_ed25519_only_known_hosts() {
	begin "ed25519 only known_hosts connects to a bastion with several key types"

	local known_hosts="$TMPDIR_E2E/ed25519_known_hosts"
	if ! record_host_key "$known_hosts" -t ed25519; then
		fail "could not record the ed25519 host key"
		return
	fi

	if grep -q -e "ssh-rsa" -e "ecdsa" "$known_hosts"; then
		fail "known_hosts must hold the ed25519 key only"
		return
	fi

	local config="$TMPDIR_E2E/ed25519_only.yml"
	config_with "known_hosts_path: $known_hosts" "$config"

	local log_file="$TMPDIR_E2E/ed25519_only.log"
	start_mogura "$log_file" -config "$config"

	local code
	code=$(http_code)
	if [ "$code" != "200" ]; then
		log "mogura output:"
		sed 's/^/  /' "$log_file"
		fail "want http 200 through the tunnel, got $code"
	else
		pass
	fi

	stop_mogura
}

main() {
	check_prerequisites

	TMPDIR_E2E=$(mktemp -d) || abort "could not create a temporary directory."

	log "--- launching the test containers"
	compose_started=1
	if ! docker compose -f "$COMPOSE_FILE" up -d; then
		abort "could not launch the containers."
	fi

	log "--- waiting for the bastion sshd"
	if ! wait_for_sshd; then
		abort "the bastion did not answer on ${BASTION_HOST}:${BASTION_PORT}."
	fi
	log ""

	scenario_verified_tunnel
	scenario_unknown_host
	scenario_changed_host_key
	scenario_insecure_option
	scenario_insecure_config
	scenario_ed25519_only_known_hosts

	if [ "$failures" -ne 0 ]; then
		log "e2e failed: $failures scenario(s)"
		return 1
	fi

	log "e2e passed"
	return 0
}

main "$@"
