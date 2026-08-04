#!/usr/bin/env bash

set -euo pipefail

p2pnc_usage() {
	cat <<'EOF'
Run p2p-nc outside a WireGuard full-tunnel route on Linux.

Usage:
  sudo wireguard-full-tunnel.sh [--uid UID] [--priority PRIORITY] [--home DIR] -- COMMAND [ARG...]

The wrapper assigns COMMAND a dedicated numeric UID and adds IPv4/IPv6 policy
rules that send every socket owned by that UID through the physical main route.
This prevents signaling, discovery, and P2P carrier packets from recursively
entering the WireGuard tunnel that p2p-nc itself transports.

Options:
  --uid UID           Dedicated numeric UID. By default, select an unused UID.
  --priority NUMBER   ip-rule priority. By default, select a free 10000..10999.
  --home DIR          Writable HOME for COMMAND. By default, use a temporary dir.
  -h, --help          Show this help.

Example:
  sudo ./deploy/wireguard-full-tunnel.sh -- \
    /usr/local/bin/p2p-nc -u --udp-idle-timeout 0 -p 15182 \
    12D3KooWJ7satLo5LXjhSZBMVTWRG1AZ77sQYtX81qHHf2VtscdL 35182
EOF
}

p2pnc_die() {
	printf '[p2p-nc full-tunnel] %s\n' "$*" >&2
	exit 1
}

p2pnc_require_command() {
	command -v "$1" >/dev/null 2>&1 || p2pnc_die "required command not found: $1"
}

p2pnc_uid=""
p2pnc_priority=""
p2pnc_home=""
while (($# > 0)); do
	case "$1" in
	--uid)
		(($# >= 2)) || p2pnc_die "--uid requires a value"
		p2pnc_uid="$2"
		shift 2
		;;
	--priority)
		(($# >= 2)) || p2pnc_die "--priority requires a value"
		p2pnc_priority="$2"
		shift 2
		;;
	--home)
		(($# >= 2)) || p2pnc_die "--home requires a value"
		p2pnc_home="$2"
		shift 2
		;;
	-h | --help)
		p2pnc_usage
		exit 0
		;;
	--)
		shift
		break
		;;
	*)
		p2pnc_die "unknown option: $1"
		;;
	esac
done

(($# > 0)) || p2pnc_die "COMMAND is required after --"
[[ "$(uname -s)" == "Linux" ]] || p2pnc_die "this wrapper requires Linux policy routing"
[[ "$(id -u)" == "0" ]] || p2pnc_die "run this wrapper as root (for example with sudo)"

for p2pnc_dependency in ip setpriv mktemp chown; do
	p2pnc_require_command "${p2pnc_dependency}"
done

if [[ -n "${p2pnc_uid}" ]]; then
	[[ "${p2pnc_uid}" =~ ^[0-9]+$ ]] || p2pnc_die "--uid must be numeric"
	((p2pnc_uid >= 1 && p2pnc_uid <= 4294967294)) || p2pnc_die "--uid is outside the Linux UID range"
else
	p2pnc_process_uids="$(ps -eo uid= 2>/dev/null | tr -d ' ' | sort -u || true)"
	for ((p2pnc_candidate = 64999; p2pnc_candidate >= 60000; p2pnc_candidate--)); do
		if ! getent passwd "${p2pnc_candidate}" >/dev/null 2>&1 &&
			! grep -qx "${p2pnc_candidate}" <<<"${p2pnc_process_uids}"; then
			p2pnc_uid="${p2pnc_candidate}"
			break
		fi
	done
	[[ -n "${p2pnc_uid}" ]] || p2pnc_die "could not find an unused UID in 60000..64999"
fi

if [[ -n "${p2pnc_priority}" ]]; then
	[[ "${p2pnc_priority}" =~ ^[0-9]+$ ]] || p2pnc_die "--priority must be numeric"
	((p2pnc_priority >= 1 && p2pnc_priority <= 32764)) || p2pnc_die "--priority must be between 1 and 32764"
	p2pnc_ipv4_rules="$(ip -4 rule show)"
	p2pnc_ipv6_rules="$(ip -6 rule show 2>/dev/null || true)"
	if grep -Eq "^${p2pnc_priority}:|^$((p2pnc_priority + 1)):" <<<"${p2pnc_ipv4_rules}" ||
		grep -Eq "^${p2pnc_priority}:|^$((p2pnc_priority + 1)):" <<<"${p2pnc_ipv6_rules}"; then
		p2pnc_die "ip-rule priority ${p2pnc_priority} or $((p2pnc_priority + 1)) is already in use"
	fi
else
	p2pnc_ipv4_rules="$(ip -4 rule show)"
	p2pnc_ipv6_rules="$(ip -6 rule show 2>/dev/null || true)"
	for ((p2pnc_candidate = 10000; p2pnc_candidate <= 10998; p2pnc_candidate++)); do
		if ! grep -Eq "^${p2pnc_candidate}:" <<<"${p2pnc_ipv4_rules}" &&
			! grep -Eq "^${p2pnc_candidate}:" <<<"${p2pnc_ipv6_rules}" &&
			! grep -Eq "^$((p2pnc_candidate + 1)):" <<<"${p2pnc_ipv4_rules}" &&
			! grep -Eq "^$((p2pnc_candidate + 1)):" <<<"${p2pnc_ipv6_rules}"; then
			p2pnc_priority="${p2pnc_candidate}"
			break
		fi
	done
	[[ -n "${p2pnc_priority}" ]] || p2pnc_die "could not find a free ip-rule priority in 10000..10999"
fi

ip -4 route show table main default | grep -q . ||
	p2pnc_die "the main IPv4 routing table has no physical default route"

p2pnc_temp_home=""
if [[ -z "${p2pnc_home}" ]]; then
	p2pnc_temp_home="$(mktemp -d /tmp/p2p-netcat-full-tunnel.XXXXXX)"
	p2pnc_home="${p2pnc_temp_home}"
else
	[[ -d "${p2pnc_home}" ]] || p2pnc_die "--home is not a directory: ${p2pnc_home}"
fi
chown "${p2pnc_uid}:${p2pnc_uid}" "${p2pnc_home}"
chmod 0700 "${p2pnc_home}"

p2pnc_ipv4_rule_added=0
p2pnc_ipv6_rule_added=0
p2pnc_ipv4_guard_added=0
p2pnc_ipv6_guard_added=0
p2pnc_guard_priority=$((p2pnc_priority + 1))
p2pnc_child_pid=""
p2pnc_cleanup() {
	if [[ "${p2pnc_ipv6_rule_added}" == "1" ]]; then
		if [[ "${p2pnc_ipv6_guard_added}" == "1" ]]; then
			ip -6 rule del priority "${p2pnc_guard_priority}" uidrange "${p2pnc_uid}-${p2pnc_uid}" prohibit >/dev/null 2>&1 || true
		fi
		ip -6 rule del priority "${p2pnc_priority}" uidrange "${p2pnc_uid}-${p2pnc_uid}" lookup main >/dev/null 2>&1 || true
	fi
	if [[ "${p2pnc_ipv4_rule_added}" == "1" ]]; then
		if [[ "${p2pnc_ipv4_guard_added}" == "1" ]]; then
			ip -4 rule del priority "${p2pnc_guard_priority}" uidrange "${p2pnc_uid}-${p2pnc_uid}" prohibit >/dev/null 2>&1 || true
		fi
		ip -4 rule del priority "${p2pnc_priority}" uidrange "${p2pnc_uid}-${p2pnc_uid}" lookup main >/dev/null 2>&1 || true
	fi
	if [[ -n "${p2pnc_temp_home}" && -d "${p2pnc_temp_home}" ]]; then
		rm -rf -- "${p2pnc_temp_home}"
	fi
}
p2pnc_forward_signal() {
	if [[ -n "${p2pnc_child_pid}" ]]; then
		kill -s "$1" "${p2pnc_child_pid}" >/dev/null 2>&1 || true
	fi
}
trap p2pnc_cleanup EXIT
trap 'p2pnc_forward_signal INT' INT
trap 'p2pnc_forward_signal TERM' TERM
trap 'p2pnc_forward_signal HUP' HUP

ip -4 rule add priority "${p2pnc_priority}" uidrange "${p2pnc_uid}-${p2pnc_uid}" lookup main
p2pnc_ipv4_rule_added=1
ip -4 rule add priority "${p2pnc_guard_priority}" uidrange "${p2pnc_uid}-${p2pnc_uid}" prohibit
p2pnc_ipv4_guard_added=1
if ip -6 rule add priority "${p2pnc_priority}" uidrange "${p2pnc_uid}-${p2pnc_uid}" lookup main 2>/dev/null; then
	p2pnc_ipv6_rule_added=1
	ip -6 rule add priority "${p2pnc_guard_priority}" uidrange "${p2pnc_uid}-${p2pnc_uid}" prohibit
	p2pnc_ipv6_guard_added=1
fi

printf '[p2p-nc full-tunnel] UID %s bypasses WireGuard through table main (priorities %s-%s)\n' \
	"${p2pnc_uid}" "${p2pnc_priority}" "${p2pnc_guard_priority}" >&2

set +e
HOME="${p2pnc_home}" XDG_CONFIG_HOME="${p2pnc_home}/.config" \
	setpriv --reuid "${p2pnc_uid}" --regid "${p2pnc_uid}" --clear-groups -- "$@" &
p2pnc_child_pid=$!
wait "${p2pnc_child_pid}"
p2pnc_status=$?
set -e
exit "${p2pnc_status}"
