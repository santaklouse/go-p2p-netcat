#!/usr/bin/env bash

set -euo pipefail

p2pnc_test_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
p2pnc_test_tmp="$(mktemp -d)"
p2pnc_test_cleanup() {
	rm -rf -- "${p2pnc_test_tmp}"
}
trap p2pnc_test_cleanup EXIT

p2pnc_test_bin="${p2pnc_test_tmp}/bin"
p2pnc_test_log="${p2pnc_test_tmp}/commands.log"
mkdir -p "${p2pnc_test_bin}"

cat >"${p2pnc_test_bin}/uname" <<'EOF'
#!/usr/bin/env bash
printf 'Linux\n'
EOF
cat >"${p2pnc_test_bin}/id" <<'EOF'
#!/usr/bin/env bash
[[ "${1:-}" == "-u" ]] && printf '0\n'
EOF
cat >"${p2pnc_test_bin}/ip" <<'EOF'
#!/usr/bin/env bash
printf 'ip %s\n' "$*" >>"${P2PNC_TEST_LOG}"
if [[ "$*" == *"route show table main default"* ]]; then
	printf 'default via 192.0.2.1 dev eth0\n'
fi
EOF
cat >"${p2pnc_test_bin}/chown" <<'EOF'
#!/usr/bin/env bash
printf 'chown %s\n' "$*" >>"${P2PNC_TEST_LOG}"
EOF
cat >"${p2pnc_test_bin}/setpriv" <<'EOF'
#!/usr/bin/env bash
printf 'setpriv %s\n' "$*" >>"${P2PNC_TEST_LOG}"
while (($# > 0)) && [[ "$1" != "--" ]]; do shift; done
shift
exec "$@"
EOF
cat >"${p2pnc_test_bin}/probe" <<'EOF'
#!/usr/bin/env bash
printf 'HOME=%s XDG_CONFIG_HOME=%s ARGS=%s\n' "$HOME" "$XDG_CONFIG_HOME" "$*" >>"${P2PNC_TEST_LOG}"
EOF
chmod +x "${p2pnc_test_bin}"/*

P2PNC_TEST_LOG="${p2pnc_test_log}" \
	PATH="${p2pnc_test_bin}:/usr/bin:/bin" \
	bash "${p2pnc_test_root}/deploy/wireguard-full-tunnel.sh" \
		--uid 62000 --priority 10000 -- probe alpha "two words"

grep -Fq 'ip -4 rule add priority 10000 uidrange 62000-62000 lookup main' "${p2pnc_test_log}"
grep -Fq 'ip -6 rule add priority 10000 uidrange 62000-62000 lookup main' "${p2pnc_test_log}"
grep -Fq 'ip -4 rule add priority 10001 uidrange 62000-62000 prohibit' "${p2pnc_test_log}"
grep -Fq 'ip -6 rule add priority 10001 uidrange 62000-62000 prohibit' "${p2pnc_test_log}"
grep -Fq 'setpriv --reuid 62000 --regid 62000 --clear-groups -- probe alpha two words' "${p2pnc_test_log}"
grep -Eq 'HOME=/tmp/p2p-netcat-full-tunnel\.[^ ]+ XDG_CONFIG_HOME=/tmp/p2p-netcat-full-tunnel\.[^ ]+/.config ARGS=alpha two words' "${p2pnc_test_log}"
grep -Fq 'ip -4 rule del priority 10000 uidrange 62000-62000 lookup main' "${p2pnc_test_log}"
grep -Fq 'ip -6 rule del priority 10000 uidrange 62000-62000 lookup main' "${p2pnc_test_log}"
grep -Fq 'ip -4 rule del priority 10001 uidrange 62000-62000 prohibit' "${p2pnc_test_log}"
grep -Fq 'ip -6 rule del priority 10001 uidrange 62000-62000 prohibit' "${p2pnc_test_log}"

if PATH="${p2pnc_test_bin}:/usr/bin:/bin" bash \
	"${p2pnc_test_root}/deploy/wireguard-full-tunnel.sh" --uid invalid -- probe; then
	printf 'invalid UID unexpectedly succeeded\n' >&2
	exit 1
fi

printf '%s\n' 'WireGuard full-tunnel wrapper tests passed'
