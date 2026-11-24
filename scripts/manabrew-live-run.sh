#!/usr/bin/env bash
# manabrew-live-run.sh -- a local ManaBrew relay plus a headless bot-hosting node, for a live
# client (four-yay's cmd/xclient -engine manabrew) to join.
#
#   scripts/manabrew-live-run.sh             # build, start both, print relay URL + room, wait; INT/TERM stops both
#   scripts/manabrew-live-run.sh --selftest  # build, start, check both listen (ss -lptn), stop; exit 0 when they do
#
# What it runs: manabrew-server (the relay) and self-hosted-node, from MANABREW_LIVE_REPO at
# MANABREW_LIVE_REF (file MANABREW_LIVE_PIN). Both listen on free loopback ports, share a random
# MANABREW_SERVER_KEY, and the node runs with SELF_HOSTED_NODE_BOT_ENABLED=1: it hosts a room and
# seats ManaBrew's bot in it, so a client that joins the room plays the bot.
#
# Out of tree like manabrew-oracle-setup.sh (ManaBrew is AGPL-3.0): the same dev clone, cargo home
# and target dir under $MANABREW_ORACLE_DIR (default ${XDG_CACHE_HOME:-$HOME/.cache}/gorge-compliance/manabrew),
# a detached worktree live-<ref12>/, and logs/pids/keys under $MANABREW_ORACLE_DIR/live/. The Forge
# card archive is the one `make manabrew-oracle-setup` builds; run that first.
#
# The key is passed to the children by environment only, never on a command line, and is written
# to live/server.key (mode 600) for the client run: export MANABREW_SERVER_KEY=$(cat <that>).
#
# Env: CARDSET_ARCHIVE overrides the archive path; MANABREW_LIVE_REPO / MANABREW_LIVE_REF override the pin; SELF_HOSTED_NODE_DECK,
# SELF_HOSTED_NODE_BOT_DECK, PRESET_DECKS_DIR, SELF_HOSTED_NODE_MAX_GAMES, SELF_HOSTED_NODE_FORMAT
# pass through to the node; CARGO, XENGINE_SCOPE=none as in manabrew-oracle-setup.sh;
# LIVE_START_TIMEOUT (seconds, default 120).
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
die() { echo "manabrew-live-run: $*" >&2; exit 2; }
mk() { sed -n "s/^$1 *?= *//p" "$here/Makefile"; }
pin() { sed -n "s/^$1=//p" "$here/MANABREW_LIVE_PIN" | tail -n 1; }
gorge_mk() {
	local g
	g=$(cd "$here" && go list -m -f '{{.Dir}}' github.com/adams-shaun/gorge)
	sed -n "s/^$1 *?= *//p" "$g/Makefile"
}

selftest=0
case "${1:-}" in
--selftest) selftest=1; [ $# -eq 1 ] || die "usage: [--selftest]" ;;
"") ;;
*) die "usage: [--selftest]" ;;
esac

repo=${MANABREW_LIVE_REPO:-$(pin MANABREW_LIVE_REPO)}
ref=${MANABREW_LIVE_REF:-$(pin MANABREW_LIVE_REF)}
[ -n "$repo" ] && [ -n "$ref" ] || die "MANABREW_LIVE_REPO / MANABREW_LIVE_REF unset (see MANABREW_LIVE_PIN)"
cache=${FOLLOWERS_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/gorge-compliance}
dir=$(realpath -m "${MANABREW_ORACLE_DIR:-$cache/manabrew}")
real_here=$(realpath -m "$here")
case "$dir" in
"$real_here" | "$real_here"/*) die "$dir is inside the repository; ManaBrew source and build output are AGPL material and are never committed" ;;
/tmp/*) die "$dir is under /tmp, which is often RAM-backed; the build needs a directory on disk" ;;
esac
forge_ref=${FORGE_REF:-$(gorge_mk FORGE_REF)}
archive=${CARDSET_ARCHIVE:-$dir/cardset-${forge_ref:0:12}.rkyv}
[ -s "$archive" ] || die "no card archive $archive; run make manabrew-oracle-setup first (or set CARDSET_ARCHIVE to an older one)"

clone=$dir/manabrew
live=$dir/live
mkdir -p "$dir" "$live"
[ -e "$clone/.git" ] || git clone -q --filter=blob:none "$repo" "$clone"
if ! oref=$(git -C "$clone" rev-parse --verify -q "$ref^{commit}"); then
	git -C "$clone" fetch -q "$repo" "$ref" 2>/dev/null || git -C "$clone" fetch -q origin
	oref=$(git -C "$clone" rev-parse --verify -q "$ref^{commit}") || die "MANABREW_LIVE_REF $ref is not in $clone after fetch of $repo"
fi
build=$dir/live-${oref:0:12}
if [ ! -e "$build/.git" ]; then
	git -C "$clone" worktree add --detach "$build" "$oref"
fi
[ "$(git -C "$build" rev-parse HEAD)" = "$oref" ] || die "$build is not at $oref"

cargo=${CARGO:-$HOME/.cargo/bin/cargo}
PATH=$(dirname "$cargo"):$PATH
export CARGO_HOME=$cache/cargo-home CARGO_TARGET_DIR=$dir/target
mkdir -p "$CARGO_HOME"
scope=()
if [ "${XENGINE_SCOPE:-}" != none ] && command -v systemd-run >/dev/null 2>&1; then
	scope=(systemd-run --user --scope -q -p MemoryMax=12G -p CPUQuota=400%)
fi
(cd "$build/manabrew-rs" &&
	"$here/scripts/heavy-lock.sh" --wait 20000 --mem 12G --name manabrew-live-run -- "${scope[@]}" \
		"$cargo" build --locked --release -p manabrew-server -p self-hosted-node)
server_bin=$CARGO_TARGET_DIR/release/manabrew-server
node_bin=$CARGO_TARGET_DIR/release/self-hosted-node
[ -x "$server_bin" ] && [ -x "$node_bin" ] || die "cargo build left no $server_bin / $node_bin"

free_port() {
	python3 -I -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])'
}
port=$(free_port)
health=$(free_port)
art=$(free_port)
[ "$port" != "$health" ] && [ "$port" != "$art" ] && [ "$health" != "$art" ] || die "free_port returned a duplicate; rerun"

key=$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')
( umask 077; printf '%s\n' "$key" > "$live/server.key" )
relay_url=ws://127.0.0.1:$port
room_name=live-$$
server_log=$live/server.log
node_log=$live/node.log
: > "$server_log"; : > "$node_log"
server_pid= node_pid=

stop_pid() {
	local p=$1
	[ -n "$p" ] || return 0
	kill "$p" 2>/dev/null || return 0
	for _ in $(seq 1 50); do
		kill -0 "$p" 2>/dev/null || return 0
		sleep 0.1
	done
	kill -9 "$p" 2>/dev/null || true
}
cleanup() {
	trap - EXIT INT TERM
	stop_pid "$node_pid"
	stop_pid "$server_pid"
	rm -f "$live/server.pid" "$live/node.pid"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

MANABREW_SERVER_KEY=$key FORGE_HOST=127.0.0.1 FORGE_PORT=$port FORGE_HEALTH_PORT=$health MANABREW_ART_PORT=$art \
	"$server_bin" >> "$server_log" 2>&1 &
server_pid=$!
echo "$server_pid" > "$live/server.pid"

listens() { # pid port: does pid hold a listening socket on loopback port?
	ss -lptn "sport = :$2" 2>/dev/null | /usr/bin/grep -q "pid=$1,"
}
deadline=$((SECONDS + ${LIVE_START_TIMEOUT:-120}))
until listens "$server_pid" "$port"; do
	kill -0 "$server_pid" 2>/dev/null || { tail -n 20 "$server_log" >&2; die "manabrew-server exited; log $server_log"; }
	[ "$SECONDS" -lt "$deadline" ] || die "manabrew-server not listening on $port after ${LIVE_START_TIMEOUT:-120}s; log $server_log"
	sleep 0.2
done
MANABREW_SERVER_KEY=$key SELF_HOSTED_NODE_RELAY_URL=$relay_url SELF_HOSTED_NODE_ROOM_NAME=$room_name \
	SELF_HOSTED_NODE_BOT_ENABLED=1 CARDSET_ARCHIVE=$archive \
	PRESET_DECKS_DIR=${PRESET_DECKS_DIR:-$build/public/preset_decks} \
	"$node_bin" >> "$node_log" 2>&1 &
node_pid=$!
echo "$node_pid" > "$live/node.pid"

# The node connects out to the relay and creates a room; it holds no listening socket of its own
# unless it serves metrics, so "node up" is: alive and its room created.
room=
until room=$(sed 's/\x1b\[[0-9;]*m//g' "$node_log" | sed -n 's/.* room created room_id="\([^"]*\)".*/\1/p' | tail -n 1) && [ -n "$room" ]; do
	kill -0 "$node_pid" 2>/dev/null || { tail -n 20 "$node_log" >&2; die "self-hosted-node exited; log $node_log"; }
	[ "$SECONDS" -lt "$deadline" ] || { tail -n 20 "$node_log" >&2; die "self-hosted-node made no room after ${LIVE_START_TIMEOUT:-120}s; log $node_log"; }
	sleep 0.2
done

echo "manabrew-live-run: ref ${oref:0:12} ($repo)"
echo "manabrew-live-run: relay $relay_url (server pid $server_pid, node pid $node_pid)"
echo "manabrew-live-run: room $room (name $room_name)"
echo "manabrew-live-run: key in $live/server.key; logs $server_log $node_log"

if [ "$selftest" = 1 ]; then
	listens "$server_pid" "$port" || die "selftest: server pid $server_pid not listening on $port"
	ss -lptn "sport = :$port" | sed 's/^/manabrew-live-run: ss: /'
	kill -0 "$node_pid" 2>/dev/null || die "selftest: node pid $node_pid not running"
	# The node is a relay client, not a listener: it holds an established connection to the relay port.
	ss -ptn "dport = :$port" | /usr/bin/grep "pid=$node_pid," | sed 's/^/manabrew-live-run: ss: /' ||
		die "selftest: node pid $node_pid holds no connection to the relay on $port"
	echo "manabrew-live-run: selftest ok"
	exit 0
fi
echo "manabrew-live-run: ready; Ctrl-C stops both"
while kill -0 "$server_pid" 2>/dev/null && kill -0 "$node_pid" 2>/dev/null; do sleep 1; done
die "a child exited; logs $server_log $node_log"
