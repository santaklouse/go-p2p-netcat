#!/usr/bin/env bash

# End-to-end Linux test:
#   HTTP client -> WireGuard full tunnel -> p2p-nc UDP-over-TCP -> WireGuard
#   gateway -> HTTP server
# The two p2p-nc peers live behind separate network-namespace NAT routers and
# communicate without a relay. The server NAT has one explicit TCP port map so
# the test is deterministic and independent of public signaling services.

set -euo pipefail
umask 077

p2pnc_test_die() {
	printf '[full-tunnel test] %s\n' "$*" >&2
	exit 1
}

[[ "$(uname -s)" == "Linux" ]] || p2pnc_test_die "Linux is required"
[[ "$(id -u)" == "0" ]] || p2pnc_test_die "run as root"

for p2pnc_test_dependency in ip wg setpriv curl python3 sysctl; do
	command -v "${p2pnc_test_dependency}" >/dev/null 2>&1 ||
		p2pnc_test_die "required command not found: ${p2pnc_test_dependency}"
done
p2pnc_test_iptables="${P2PNC_IPTABLES:-iptables}"
command -v "${p2pnc_test_iptables}" >/dev/null 2>&1 ||
	p2pnc_test_die "required command not found: ${p2pnc_test_iptables}"

p2pnc_test_binary="${P2PNC_BINARY:-}"
[[ -n "${p2pnc_test_binary}" ]] || p2pnc_test_die "set P2PNC_BINARY to a Linux p2p-nc binary"
p2pnc_test_binary="$(readlink -f "${p2pnc_test_binary}")"
[[ -x "${p2pnc_test_binary}" ]] || p2pnc_test_die "binary is not executable: ${p2pnc_test_binary}"
p2pnc_test_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
p2pnc_test_wrapper="${P2PNC_FULL_TUNNEL_WRAPPER:-${p2pnc_test_root}/deploy/wireguard-full-tunnel.sh}"
p2pnc_test_wrapper="$(readlink -f "${p2pnc_test_wrapper}")"
[[ -x "${p2pnc_test_wrapper}" ]] || p2pnc_test_die "full-tunnel wrapper is not executable: ${p2pnc_test_wrapper}"

p2pnc_test_tag="$(printf '%06d' "$((BASHPID % 1000000))")"
p2pnc_test_client="pc${p2pnc_test_tag}"
p2pnc_test_client_nat="cn${p2pnc_test_tag}"
p2pnc_test_server="ps${p2pnc_test_tag}"
p2pnc_test_server_nat="sn${p2pnc_test_tag}"
p2pnc_test_internet="pi${p2pnc_test_tag}"
p2pnc_test_client_lan_host="clh${p2pnc_test_tag}"
p2pnc_test_client_lan_nat="cln${p2pnc_test_tag}"
p2pnc_test_server_lan_host="slh${p2pnc_test_tag}"
p2pnc_test_server_lan_nat="sln${p2pnc_test_tag}"
p2pnc_test_client_wan_ns="cwn${p2pnc_test_tag}"
p2pnc_test_server_wan_ns="swn${p2pnc_test_tag}"
p2pnc_test_egress_server="egs${p2pnc_test_tag}"
p2pnc_test_internet_ns="iwn${p2pnc_test_tag}"
p2pnc_test_dir="$(mktemp -d /tmp/p2p-netcat-netns.XXXXXX)"
# The p2p client intentionally runs as an otherwise unused UID. It needs to
# traverse this directory to reach its private HOME, but cannot list it.
chmod 0711 "${p2pnc_test_dir}"
mkdir "${p2pnc_test_dir}/server-state"
chmod 0700 "${p2pnc_test_dir}/server-state"
p2pnc_test_pids=()

p2pnc_test_cleanup() {
	for p2pnc_test_pid in "${p2pnc_test_pids[@]}"; do
		kill "${p2pnc_test_pid}" >/dev/null 2>&1 || true
	done
	for p2pnc_test_namespace in \
		"${p2pnc_test_client}" "${p2pnc_test_client_nat}" \
		"${p2pnc_test_server}" "${p2pnc_test_server_nat}" \
		"${p2pnc_test_internet}"; do
		ip netns del "${p2pnc_test_namespace}" >/dev/null 2>&1 || true
	done
	rm -rf -- "${p2pnc_test_dir}"
}
trap p2pnc_test_cleanup EXIT INT TERM HUP

for p2pnc_test_namespace in \
	"${p2pnc_test_client}" "${p2pnc_test_client_nat}" \
	"${p2pnc_test_server}" "${p2pnc_test_server_nat}" \
	"${p2pnc_test_internet}"; do
	ip netns add "${p2pnc_test_namespace}"
	ip -n "${p2pnc_test_namespace}" link set lo up
done

# Client private network.
ip link add "${p2pnc_test_client_lan_host}" type veth peer name "${p2pnc_test_client_lan_nat}"
ip link set "${p2pnc_test_client_lan_host}" netns "${p2pnc_test_client}"
ip link set "${p2pnc_test_client_lan_nat}" netns "${p2pnc_test_client_nat}"
ip -n "${p2pnc_test_client}" link set "${p2pnc_test_client_lan_host}" name eth0
ip -n "${p2pnc_test_client}" addr add 10.10.1.2/24 dev eth0
ip -n "${p2pnc_test_client}" link set eth0 up
ip -n "${p2pnc_test_client}" route add default via 10.10.1.1
ip -n "${p2pnc_test_client_nat}" link set "${p2pnc_test_client_lan_nat}" name lan0
ip -n "${p2pnc_test_client_nat}" addr add 10.10.1.1/24 dev lan0
ip -n "${p2pnc_test_client_nat}" link set lan0 up

# Server private network.
ip link add "${p2pnc_test_server_lan_host}" type veth peer name "${p2pnc_test_server_lan_nat}"
ip link set "${p2pnc_test_server_lan_host}" netns "${p2pnc_test_server}"
ip link set "${p2pnc_test_server_lan_nat}" netns "${p2pnc_test_server_nat}"
ip -n "${p2pnc_test_server}" link set "${p2pnc_test_server_lan_host}" name eth0
ip -n "${p2pnc_test_server}" addr add 10.20.1.2/24 dev eth0
ip -n "${p2pnc_test_server}" link set eth0 up
ip -n "${p2pnc_test_server}" route add default via 10.20.1.1
ip -n "${p2pnc_test_server_nat}" link set "${p2pnc_test_server_lan_nat}" name lan0
ip -n "${p2pnc_test_server_nat}" addr add 10.20.1.1/24 dev lan0
ip -n "${p2pnc_test_server_nat}" link set lan0 up

# Simulated public link between the two NAT routers.
ip link add "${p2pnc_test_client_wan_ns}" type veth peer name "${p2pnc_test_server_wan_ns}"
ip link set "${p2pnc_test_client_wan_ns}" netns "${p2pnc_test_client_nat}"
ip -n "${p2pnc_test_client_nat}" link set "${p2pnc_test_client_wan_ns}" name wan0
ip -n "${p2pnc_test_client_nat}" addr add 198.18.0.2/24 dev wan0
ip -n "${p2pnc_test_client_nat}" link set wan0 up
ip link set "${p2pnc_test_server_wan_ns}" netns "${p2pnc_test_server_nat}"
ip -n "${p2pnc_test_server_nat}" link set "${p2pnc_test_server_wan_ns}" name wan0
ip -n "${p2pnc_test_server_nat}" addr add 198.18.0.3/24 dev wan0
ip -n "${p2pnc_test_server_nat}" link set wan0 up

# A separate egress network represents the Internet behind the WG gateway.
ip link add "${p2pnc_test_egress_server}" type veth peer name "${p2pnc_test_internet_ns}"
ip link set "${p2pnc_test_egress_server}" netns "${p2pnc_test_server}"
ip -n "${p2pnc_test_server}" link set "${p2pnc_test_egress_server}" name eth1
ip -n "${p2pnc_test_server}" addr add 203.0.113.1/24 dev eth1
ip -n "${p2pnc_test_server}" link set eth1 up
ip link set "${p2pnc_test_internet_ns}" netns "${p2pnc_test_internet}"
ip -n "${p2pnc_test_internet}" link set "${p2pnc_test_internet_ns}" name eth0
ip -n "${p2pnc_test_internet}" addr add 203.0.113.2/24 dev eth0
ip -n "${p2pnc_test_internet}" link set eth0 up

for p2pnc_test_nat in "${p2pnc_test_client_nat}" "${p2pnc_test_server_nat}"; do
	ip netns exec "${p2pnc_test_nat}" sysctl -q -w net.ipv4.ip_forward=1
	ip netns exec "${p2pnc_test_nat}" sysctl -q -w net.ipv4.conf.all.rp_filter=0
	ip netns exec "${p2pnc_test_nat}" sysctl -q -w net.ipv4.conf.default.rp_filter=0
	ip netns exec "${p2pnc_test_nat}" "${p2pnc_test_iptables}" -P FORWARD ACCEPT
done
ip netns exec "${p2pnc_test_client_nat}" \
	"${p2pnc_test_iptables}" -t nat -A POSTROUTING -s 10.10.1.0/24 -o wan0 -j MASQUERADE
ip netns exec "${p2pnc_test_server_nat}" \
	"${p2pnc_test_iptables}" -t nat -A POSTROUTING -s 10.20.1.0/24 -o wan0 -j MASQUERADE
ip netns exec "${p2pnc_test_server_nat}" \
	"${p2pnc_test_iptables}" -t nat -A PREROUTING -p tcp --dport 4001 \
	-j DNAT --to-destination 10.20.1.2:4001

printf 'full-tunnel-ok\n' >"${p2pnc_test_dir}/index.html"
ip netns exec "${p2pnc_test_internet}" \
	python3 -m http.server 8080 --bind 203.0.113.2 \
	--directory "${p2pnc_test_dir}" >"${p2pnc_test_dir}/http.log" 2>&1 &
p2pnc_test_pids+=("$!")

wg genkey >"${p2pnc_test_dir}/server.key"
wg genkey >"${p2pnc_test_dir}/client.key"
p2pnc_test_server_public="$(wg pubkey <"${p2pnc_test_dir}/server.key")"
p2pnc_test_client_public="$(wg pubkey <"${p2pnc_test_dir}/client.key")"

ip -n "${p2pnc_test_server}" link add wg0 type wireguard
ip netns exec "${p2pnc_test_server}" wg set wg0 \
	private-key "${p2pnc_test_dir}/server.key" listen-port 51820 \
	peer "${p2pnc_test_client_public}" allowed-ips 10.99.0.2/32
ip -n "${p2pnc_test_server}" addr add 10.99.0.1/24 dev wg0
ip -n "${p2pnc_test_server}" link set wg0 up
ip netns exec "${p2pnc_test_server}" sysctl -q -w net.ipv4.ip_forward=1
ip netns exec "${p2pnc_test_server}" "${p2pnc_test_iptables}" -P FORWARD ACCEPT
ip netns exec "${p2pnc_test_server}" \
	"${p2pnc_test_iptables}" -t nat -A POSTROUTING -s 10.99.0.0/24 -o eth1 -j MASQUERADE

p2pnc_test_peer_id="$(ip netns exec "${p2pnc_test_server}" \
	"${p2pnc_test_binary}" id -I "${p2pnc_test_dir}/server-state/identity")"
ip netns exec "${p2pnc_test_server}" "${p2pnc_test_binary}" \
	-u --udp-idle-timeout 0 -l -k -4 \
	--identity "${p2pnc_test_dir}/server-state/identity" \
	--transport-port 4001 --no-dht --no-mdns --no-pubsub --no-quic --no-webrtc \
	-d 127.0.0.1 -p 51820 35182 \
	>"${p2pnc_test_dir}/server.log" 2>&1 &
p2pnc_test_pids+=("$!")

for _ in {1..100}; do
	grep -q 'listener:35182 PeerId:' "${p2pnc_test_dir}/server.log" && break
	sleep 0.05
done
grep -q 'listener:35182 PeerId:' "${p2pnc_test_dir}/server.log" || {
	cat "${p2pnc_test_dir}/server.log" >&2
	p2pnc_test_die "p2p-nc listener did not start"
}

if ! timeout 3 ip netns exec "${p2pnc_test_client}" \
	bash -c 'exec 3<>/dev/tcp/198.18.0.3/4001'; then
	printf '%s\n' '--- client routes ---' >&2
	ip -n "${p2pnc_test_client}" route show >&2 || true
	printf '%s\n' '--- client NAT rules ---' >&2
	ip netns exec "${p2pnc_test_client_nat}" "${p2pnc_test_iptables}-save" >&2 || true
	printf '%s\n' '--- server NAT rules ---' >&2
	ip netns exec "${p2pnc_test_server_nat}" "${p2pnc_test_iptables}-save" >&2 || true
	ip netns exec "${p2pnc_test_server_nat}" "${p2pnc_test_iptables}" -t nat -L -n -v >&2 || true
	ip netns exec "${p2pnc_test_server_nat}" "${p2pnc_test_iptables}" -L -n -v >&2 || true
	printf '%s\n' '--- server NAT routes and forwarding ---' >&2
	ip -n "${p2pnc_test_server_nat}" route show >&2 || true
	ip netns exec "${p2pnc_test_server_nat}" sysctl net.ipv4.ip_forward >&2 || true
	printf '%s\n' '--- server listeners ---' >&2
	ip netns exec "${p2pnc_test_server}" ss -lnt >&2 || true
	p2pnc_test_die "the deterministic TCP route across both NATs is unavailable"
fi

mkdir "${p2pnc_test_dir}/client-home"
ip netns exec "${p2pnc_test_client}" \
	"${p2pnc_test_wrapper}" --uid 62000 --priority 10000 \
	--home "${p2pnc_test_dir}/client-home" -- \
	"${p2pnc_test_binary}" -u --udp-idle-timeout 0 -p 15182 \
	--no-dht --no-mdns --no-pubsub --no-quic --no-webrtc \
	"/ip4/198.18.0.3/tcp/4001/p2p/${p2pnc_test_peer_id}" 35182 \
	>"${p2pnc_test_dir}/client.log" 2>&1 &
p2pnc_test_pids+=("$!")

for _ in {1..200}; do
	grep -q 'P2P UDP carrier established' "${p2pnc_test_dir}/client.log" && break
	sleep 0.05
done
grep -q 'P2P UDP carrier established' "${p2pnc_test_dir}/client.log" || {
	cat "${p2pnc_test_dir}/client.log" >&2
	p2pnc_test_die "p2p-nc UDP carrier was not established across the two NATs"
}

ip -n "${p2pnc_test_client}" link add wg0 type wireguard
ip netns exec "${p2pnc_test_client}" wg set wg0 \
	private-key "${p2pnc_test_dir}/client.key" \
	peer "${p2pnc_test_server_public}" allowed-ips 0.0.0.0/0 \
	endpoint 127.0.0.1:15182 persistent-keepalive 5
ip -n "${p2pnc_test_client}" addr add 10.99.0.2/24 dev wg0
ip -n "${p2pnc_test_client}" link set wg0 up
ip -n "${p2pnc_test_client}" route add default dev wg0 table 51820
ip -n "${p2pnc_test_client}" rule add priority 20000 table main suppress_prefixlength 0
ip -n "${p2pnc_test_client}" rule add priority 20001 table 51820

p2pnc_test_response=""
for _ in {1..100}; do
	if p2pnc_test_response="$(ip netns exec "${p2pnc_test_client}" \
		curl --fail --silent --show-error --max-time 1 http://203.0.113.2:8080/ 2>/dev/null)"; then
		break
	fi
	sleep 0.1
done
[[ "${p2pnc_test_response}" == "full-tunnel-ok" ]] || {
	printf '%s\n' '--- client log ---' >&2
	cat "${p2pnc_test_dir}/client.log" >&2
	printf '%s\n' '--- server log ---' >&2
	cat "${p2pnc_test_dir}/server.log" >&2
	ip netns exec "${p2pnc_test_client}" wg show >&2 || true
	p2pnc_test_die "HTTP did not cross the WireGuard/p2p-nc full tunnel"
}

p2pnc_test_handshake="$(ip netns exec "${p2pnc_test_client}" wg show wg0 latest-handshakes | awk '{print $2}')"
[[ "${p2pnc_test_handshake}" =~ ^[1-9][0-9]*$ ]] || p2pnc_test_die "WireGuard handshake was not observed"

printf '%s\n' 'Full-tunnel test passed: two NATs, no relay, UDP-over-TCP, WireGuard default route, HTTP egress'
