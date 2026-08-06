#!/usr/bin/env bash

set -euo pipefail

p2pnc_docker_test_root="$(
	cd "$(dirname "${BASH_SOURCE[0]}")/.."
	pwd
)"
p2pnc_docker_test_image="p2p-netcat:local-test-$$"
p2pnc_docker_test_volume="p2p-netcat-local-test-$$"

p2pnc_docker_test_cleanup() {
	docker image rm --force "${p2pnc_docker_test_image}" >/dev/null 2>&1 || true
	docker volume rm --force "${p2pnc_docker_test_volume}" >/dev/null 2>&1 || true
}
trap p2pnc_docker_test_cleanup EXIT

docker build \
	--build-arg VERSION=docker-test \
	--tag "${p2pnc_docker_test_image}" \
	"${p2pnc_docker_test_root}"

p2pnc_docker_test_version="$(
	docker run --rm "${p2pnc_docker_test_image}" --version
)"
[[ "${p2pnc_docker_test_version}" == "p2p-nc version docker-test" ]]

for p2pnc_docker_test_alias_path in \
	/usr/local/bin/pnc \
	/usr/local/bin/p2p-netcat; do
	p2pnc_docker_test_alias="$(
		docker run --rm \
			--entrypoint "${p2pnc_docker_test_alias_path}" \
			"${p2pnc_docker_test_image}" \
			--version
	)"
	[[ "${p2pnc_docker_test_alias}" == "p2p-nc version docker-test" ]]
done

p2pnc_docker_test_config_user="$(
	docker image inspect \
		--format '{{.Config.User}}' \
		"${p2pnc_docker_test_image}"
)"
[[ "${p2pnc_docker_test_config_user}" == "65532:65532" ]]

p2pnc_docker_test_source="$(
	docker image inspect \
		--format '{{index .Config.Labels "org.opencontainers.image.source"}}' \
		"${p2pnc_docker_test_image}"
)"
[[ "${p2pnc_docker_test_source}" == "https://github.com/santaklouse/go-p2p-netcat" ]]

p2pnc_docker_test_uid="$(
	docker run --rm \
		--entrypoint /bin/sh \
		"${p2pnc_docker_test_image}" \
		-c 'id -u'
)"
[[ "${p2pnc_docker_test_uid}" == "65532" ]]

p2pnc_docker_test_cache_home="$(
	docker run --rm \
		--entrypoint /bin/sh \
		"${p2pnc_docker_test_image}" \
		-c 'printf %s "${XDG_CACHE_HOME}"'
)"
[[ "${p2pnc_docker_test_cache_home}" == "/config/p2p-netcat/cache" ]]

docker volume create "${p2pnc_docker_test_volume}" >/dev/null
p2pnc_docker_test_first_id="$(
	docker run --rm \
		--volume "${p2pnc_docker_test_volume}:/config" \
		"${p2pnc_docker_test_image}" \
		id
)"
p2pnc_docker_test_second_id="$(
	docker run --rm \
		--volume "${p2pnc_docker_test_volume}:/config" \
		"${p2pnc_docker_test_image}" \
		id
)"
[[ "${p2pnc_docker_test_first_id}" == "${p2pnc_docker_test_second_id}" ]]

docker run --rm \
	--volume "${p2pnc_docker_test_volume}:/config" \
	--entrypoint /bin/sh \
	"${p2pnc_docker_test_image}" \
	-c '
		test "$(stat -c %a /config/p2p-netcat/identity.key)" = "600"
		mkdir -p "${XDG_CACHE_HOME}/p2p-netcat/listeners"
		touch "${XDG_CACHE_HOME}/p2p-netcat/listeners/35182.lock"
		test -w "${XDG_CACHE_HOME}/p2p-netcat/listeners/35182.lock"
	'

printf '%s\n' "Docker image tests passed"
