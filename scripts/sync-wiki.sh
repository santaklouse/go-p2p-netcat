#!/usr/bin/env bash

set -Eeuo pipefail

p2pnc_wiki_repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
p2pnc_wiki_destination="${1:-}"
p2pnc_wiki_temp_dir=""

p2pnc_wiki_die() {
	printf 'sync-wiki: %s\n' "$*" >&2
	exit 1
}

p2pnc_wiki_cleanup() {
	if [[ -n "${p2pnc_wiki_temp_dir}" && -d "${p2pnc_wiki_temp_dir}" ]]; then
		rm -rf -- "${p2pnc_wiki_temp_dir}"
	fi
}

trap p2pnc_wiki_cleanup EXIT

[[ -n "${p2pnc_wiki_destination}" ]] ||
	p2pnc_wiki_die "usage: scripts/sync-wiki.sh /path/to/go-p2p-netcat.wiki"
[[ -d "${p2pnc_wiki_destination}/.git" ]] ||
	p2pnc_wiki_die "destination is not a Git repository: ${p2pnc_wiki_destination}"

p2pnc_wiki_remote="$(git -C "${p2pnc_wiki_destination}" config --get remote.origin.url || true)"
case "${p2pnc_wiki_remote}" in
*santaklouse/go-p2p-netcat.wiki.git) ;;
*) p2pnc_wiki_die "unexpected Wiki remote: ${p2pnc_wiki_remote}" ;;
esac

p2pnc_wiki_temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/p2p-netcat-wiki.XXXXXXXX")"

p2pnc_wiki_convert() {
	local source="$1"
	local destination="$2"

	sed \
		-e 's|(docs/p2p-netcat-product-technical-overview-en-mobile.pdf)|(https://santaklouse.github.io/go-p2p-netcat/docs/p2p-netcat-product-technical-overview-en-mobile.pdf)|g' \
		-e 's|(docs/p2p-netcat-product-technical-overview-ru-mobile.pdf)|(https://santaklouse.github.io/go-p2p-netcat/docs/p2p-netcat-product-technical-overview-ru-mobile.pdf)|g' \
		-e 's|(p2p-netcat-product-technical-overview-en-mobile.pdf)|(https://santaklouse.github.io/go-p2p-netcat/docs/p2p-netcat-product-technical-overview-en-mobile.pdf)|g' \
		-e 's|(p2p-netcat-product-technical-overview-ru-mobile.pdf)|(https://santaklouse.github.io/go-p2p-netcat/docs/p2p-netcat-product-technical-overview-ru-mobile.pdf)|g' \
		-e 's|(docs/p2p-netcat-product-technical-overview-en.pdf)|(https://santaklouse.github.io/go-p2p-netcat/docs/p2p-netcat-product-technical-overview-en.pdf)|g' \
		-e 's|(docs/p2p-netcat-product-technical-overview-en.pptx)|(https://santaklouse.github.io/go-p2p-netcat/docs/p2p-netcat-product-technical-overview-en.pptx)|g' \
		-e 's|(docs/p2p-netcat-product-technical-overview-ru.pdf)|(https://santaklouse.github.io/go-p2p-netcat/docs/p2p-netcat-product-technical-overview-ru.pdf)|g' \
		-e 's|(docs/p2p-netcat-product-technical-overview-ru.pptx)|(https://santaklouse.github.io/go-p2p-netcat/docs/p2p-netcat-product-technical-overview-ru.pptx)|g' \
		-e 's|(p2p-netcat-product-technical-overview-en.pdf)|(https://santaklouse.github.io/go-p2p-netcat/docs/p2p-netcat-product-technical-overview-en.pdf)|g' \
		-e 's|(p2p-netcat-product-technical-overview-en.pptx)|(https://santaklouse.github.io/go-p2p-netcat/docs/p2p-netcat-product-technical-overview-en.pptx)|g' \
		-e 's|(p2p-netcat-product-technical-overview-ru.pdf)|(https://santaklouse.github.io/go-p2p-netcat/docs/p2p-netcat-product-technical-overview-ru.pdf)|g' \
		-e 's|(p2p-netcat-product-technical-overview-ru.pptx)|(https://santaklouse.github.io/go-p2p-netcat/docs/p2p-netcat-product-technical-overview-ru.pptx)|g' \
		-e 's|docs/ARCHITECTURE.RU.md|Architecture-RU|g' \
		-e 's|docs/ARCHITECTURE.md|Architecture|g' \
		-e 's|docs/DATAGRAM_PROTOCOL.RU.md|Datagram-Protocol-RU|g' \
		-e 's|docs/DATAGRAM_PROTOCOL.md|Datagram-Protocol|g' \
		-e 's|docs/GS_NETCAT_COMPAT.RU.md|gs-netcat-Compatibility-RU|g' \
		-e 's|docs/GS_NETCAT_COMPAT.md|gs-netcat-Compatibility|g' \
		-e 's|docs/INSTALLATION.RU.md|Installation-RU|g' \
		-e 's|docs/INSTALLATION.md|Installation|g' \
		-e 's|docs/PAIRING_PROTOCOL.RU.md|Pairing-Protocol-RU|g' \
		-e 's|docs/PAIRING_PROTOCOL.md|Pairing-Protocol|g' \
		-e 's|docs/RELAY_API.RU.md|Relay-API-RU|g' \
		-e 's|docs/RELAY_API.md|Relay-API|g' \
		-e 's|docs/USE_CASES.RU.md|Use-Cases-RU|g' \
		-e 's|docs/USE_CASES.md|Use-Cases|g' \
		-e 's|docs/WEBRTC_MIGRATION.RU.md|WebRTC-Migration-RU|g' \
		-e 's|docs/WEBRTC_MIGRATION.md|WebRTC-Migration|g' \
		-e 's|(README.RU.md)|(Home-RU)|g' \
		-e 's|(README.md)|(Home)|g' \
		-e 's|(docs/ARCHITECTURE.RU.md)|(Architecture-RU)|g' \
		-e 's|(docs/ARCHITECTURE.md)|(Architecture)|g' \
		-e 's|(docs/DATAGRAM_PROTOCOL.RU.md)|(Datagram-Protocol-RU)|g' \
		-e 's|(docs/DATAGRAM_PROTOCOL.md)|(Datagram-Protocol)|g' \
		-e 's|(docs/GS_NETCAT_COMPAT.RU.md)|(gs-netcat-Compatibility-RU)|g' \
		-e 's|(docs/GS_NETCAT_COMPAT.md)|(gs-netcat-Compatibility)|g' \
		-e 's|(docs/INSTALLATION.RU.md)|(Installation-RU)|g' \
		-e 's|(docs/INSTALLATION.md)|(Installation)|g' \
		-e 's|(docs/PAIRING_PROTOCOL.RU.md)|(Pairing-Protocol-RU)|g' \
		-e 's|(docs/PAIRING_PROTOCOL.md)|(Pairing-Protocol)|g' \
		-e 's|(docs/RELAY_API.RU.md)|(Relay-API-RU)|g' \
		-e 's|(docs/RELAY_API.md)|(Relay-API)|g' \
		-e 's|(docs/USE_CASES.RU.md)|(Use-Cases-RU)|g' \
		-e 's|(docs/USE_CASES.md)|(Use-Cases)|g' \
		-e 's|(docs/WEBRTC_MIGRATION.RU.md)|(WebRTC-Migration-RU)|g' \
		-e 's|(docs/WEBRTC_MIGRATION.md)|(WebRTC-Migration)|g' \
		-e 's|(ARCHITECTURE.RU.md)|(Architecture-RU)|g' \
		-e 's|(ARCHITECTURE.md)|(Architecture)|g' \
		-e 's|(DATAGRAM_PROTOCOL.RU.md)|(Datagram-Protocol-RU)|g' \
		-e 's|(DATAGRAM_PROTOCOL.md)|(Datagram-Protocol)|g' \
		-e 's|(GS_NETCAT_COMPAT.RU.md)|(gs-netcat-Compatibility-RU)|g' \
		-e 's|(GS_NETCAT_COMPAT.md)|(gs-netcat-Compatibility)|g' \
		-e 's|(INSTALLATION.RU.md)|(Installation-RU)|g' \
		-e 's|(INSTALLATION.md)|(Installation)|g' \
		-e 's|(PAIRING_PROTOCOL.RU.md)|(Pairing-Protocol-RU)|g' \
		-e 's|(PAIRING_PROTOCOL.md)|(Pairing-Protocol)|g' \
		-e 's|(RELAY_API.RU.md)|(Relay-API-RU)|g' \
		-e 's|(RELAY_API.md)|(Relay-API)|g' \
		-e 's|(USE_CASES.RU.md)|(Use-Cases-RU)|g' \
		-e 's|(USE_CASES.md)|(Use-Cases)|g' \
		-e 's|(WEBRTC_MIGRATION.RU.md)|(WebRTC-Migration-RU)|g' \
		-e 's|(WEBRTC_MIGRATION.md)|(WebRTC-Migration)|g' \
		-e 's|(../Dockerfile)|(https://github.com/santaklouse/go-p2p-netcat/blob/main/Dockerfile)|g' \
		-e 's|(../deploy/deploy.sh)|(https://github.com/santaklouse/go-p2p-netcat/blob/main/deploy/deploy.sh)|g' \
		-e 's|(../scripts/docker_test.sh)|(https://github.com/santaklouse/go-p2p-netcat/blob/main/scripts/docker_test.sh)|g' \
		"${p2pnc_wiki_repo_root}/${source}" >"${p2pnc_wiki_temp_dir}/${destination}"
}

p2pnc_wiki_convert README.md Home.md
p2pnc_wiki_convert README.RU.md Home-RU.md
p2pnc_wiki_convert docs/ARCHITECTURE.md Architecture.md
p2pnc_wiki_convert docs/ARCHITECTURE.RU.md Architecture-RU.md
p2pnc_wiki_convert docs/DATAGRAM_PROTOCOL.md Datagram-Protocol.md
p2pnc_wiki_convert docs/DATAGRAM_PROTOCOL.RU.md Datagram-Protocol-RU.md
p2pnc_wiki_convert docs/GS_NETCAT_COMPAT.md gs-netcat-Compatibility.md
p2pnc_wiki_convert docs/GS_NETCAT_COMPAT.RU.md gs-netcat-Compatibility-RU.md
p2pnc_wiki_convert docs/INSTALLATION.md Installation.md
p2pnc_wiki_convert docs/INSTALLATION.RU.md Installation-RU.md
p2pnc_wiki_convert docs/PAIRING_PROTOCOL.md Pairing-Protocol.md
p2pnc_wiki_convert docs/PAIRING_PROTOCOL.RU.md Pairing-Protocol-RU.md
p2pnc_wiki_convert docs/RELAY_API.md Relay-API.md
p2pnc_wiki_convert docs/RELAY_API.RU.md Relay-API-RU.md
p2pnc_wiki_convert docs/USE_CASES.md Use-Cases.md
p2pnc_wiki_convert docs/USE_CASES.RU.md Use-Cases-RU.md
p2pnc_wiki_convert docs/WEBRTC_MIGRATION.md WebRTC-Migration.md
p2pnc_wiki_convert docs/WEBRTC_MIGRATION.RU.md WebRTC-Migration-RU.md

printf '%s\n' \
	'## English' \
	'' \
	'- [[Home]]' \
	'- [[Practical use cases|Use-Cases]]' \
	'- [[Installation]]' \
	'- [[Architecture]]' \
	'- [[Datagram forwarding protocol|Datagram-Protocol]]' \
	'- [[gs-netcat compatibility|gs-netcat-Compatibility]]' \
	'- [[Pairing protocol|Pairing-Protocol]]' \
	'- [[Relay API|Relay-API]]' \
	'- [[WebRTC migration|WebRTC-Migration]]' \
	'' \
	'## Русский' \
	'' \
	'- [[Главная|Home-RU]]' \
	'- [[Практические сценарии|Use-Cases-RU]]' \
	'- [[Установка|Installation-RU]]' \
	'- [[Архитектура|Architecture-RU]]' \
	'- [[Протокол datagram forwarding|Datagram-Protocol-RU]]' \
	'- [[Совместимость с gs-netcat|gs-netcat-Compatibility-RU]]' \
	'- [[Pairing protocol|Pairing-Protocol-RU]]' \
	'- [[Relay API|Relay-API-RU]]' \
	'- [[Миграция WebRTC|WebRTC-Migration-RU]]' \
	>"${p2pnc_wiki_temp_dir}/_Sidebar.md"

printf '%s\n' \
	'[Source repository](https://github.com/santaklouse/go-p2p-netcat) · [Releases](https://github.com/santaklouse/go-p2p-netcat/releases) · [Container](https://github.com/santaklouse/go-p2p-netcat/pkgs/container/go-p2p-netcat) · [Browser PWA](https://santaklouse.github.io/go-p2p-netcat/)' \
	>"${p2pnc_wiki_temp_dir}/_Footer.md"

for p2pnc_wiki_page in "${p2pnc_wiki_temp_dir}"/*.md; do
	install -m 0644 "${p2pnc_wiki_page}" \
		"${p2pnc_wiki_destination}/$(basename "${p2pnc_wiki_page}")"
done

printf 'Synchronized %s Wiki pages into %s\n' \
	"$(find "${p2pnc_wiki_temp_dir}" -type f -name '*.md' | wc -l | tr -d ' ')" \
	"${p2pnc_wiki_destination}"
