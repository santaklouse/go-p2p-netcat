#!/usr/bin/env bash

set -Eeuo pipefail

p2pnc_wiki_test_root="$(mktemp -d "${TMPDIR:-/tmp}/p2p-netcat-wiki-test.XXXXXXXX")"
trap 'rm -rf -- "${p2pnc_wiki_test_root}"' EXIT

p2pnc_wiki_test_destination="${p2pnc_wiki_test_root}/go-p2p-netcat.wiki"
mkdir -p "${p2pnc_wiki_test_destination}"
git -C "${p2pnc_wiki_test_destination}" init --quiet
git -C "${p2pnc_wiki_test_destination}" remote add origin \
	https://github.com/santaklouse/go-p2p-netcat.wiki.git

bash "$(dirname "$0")/sync-wiki.sh" "${p2pnc_wiki_test_destination}"

p2pnc_wiki_test_count="$(
	find "${p2pnc_wiki_test_destination}" -maxdepth 1 -type f -name '*.md' |
		wc -l |
		tr -d ' '
)"
[[ "${p2pnc_wiki_test_count}" == "20" ]]

for p2pnc_wiki_test_page in \
	Home.md \
	Home-RU.md \
	Datagram-Protocol.md \
	Datagram-Protocol-RU.md \
	Use-Cases.md \
	Use-Cases-RU.md \
	_Sidebar.md \
	_Footer.md; do
	[[ -s "${p2pnc_wiki_test_destination}/${p2pnc_wiki_test_page}" ]]
done

if grep -En '\]\((\.\./)?docs/[^)]*\.md\)' \
	"${p2pnc_wiki_test_destination}"/*.md >/dev/null; then
	printf 'Wiki output contains an unconverted docs link\n' >&2
	exit 1
fi

grep -Fq '[[Practical use cases|Use-Cases]]' \
	"${p2pnc_wiki_test_destination}/_Sidebar.md"
grep -Fq '[[Практические сценарии|Use-Cases-RU]]' \
	"${p2pnc_wiki_test_destination}/_Sidebar.md"

for p2pnc_wiki_test_page in Home.md Architecture.md; do
	grep -Fq 'https://santaklouse.github.io/go-p2p-netcat/docs/p2p-netcat-product-technical-overview-en-mobile.pdf' \
		"${p2pnc_wiki_test_destination}/${p2pnc_wiki_test_page}"
	grep -Fq 'https://santaklouse.github.io/go-p2p-netcat/docs/p2p-netcat-product-technical-overview-en.pdf' \
		"${p2pnc_wiki_test_destination}/${p2pnc_wiki_test_page}"
	grep -Fq 'https://santaklouse.github.io/go-p2p-netcat/docs/p2p-netcat-product-technical-overview-en.pptx' \
		"${p2pnc_wiki_test_destination}/${p2pnc_wiki_test_page}"
	! grep -Fq 'p2p-netcat-product-technical-overview-ru.' \
		"${p2pnc_wiki_test_destination}/${p2pnc_wiki_test_page}"
done

for p2pnc_wiki_test_page in Home-RU.md Architecture-RU.md; do
	grep -Fq 'https://santaklouse.github.io/go-p2p-netcat/docs/p2p-netcat-product-technical-overview-ru-mobile.pdf' \
		"${p2pnc_wiki_test_destination}/${p2pnc_wiki_test_page}"
	grep -Fq 'https://santaklouse.github.io/go-p2p-netcat/docs/p2p-netcat-product-technical-overview-ru.pdf' \
		"${p2pnc_wiki_test_destination}/${p2pnc_wiki_test_page}"
	grep -Fq 'https://santaklouse.github.io/go-p2p-netcat/docs/p2p-netcat-product-technical-overview-ru.pptx' \
		"${p2pnc_wiki_test_destination}/${p2pnc_wiki_test_page}"
	! grep -Fq 'p2p-netcat-product-technical-overview-en.' \
		"${p2pnc_wiki_test_destination}/${p2pnc_wiki_test_page}"
done

printf 'Wiki synchronization tests passed\n'
