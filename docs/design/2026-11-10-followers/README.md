# Followers: working notes

The plan is [`../2026-11-10-phase-manabrew-followers.md`](../2026-11-10-phase-manabrew-followers.md);
the files here are the audits and probes it produced.

## Live ManaBrew harness (`scripts/manabrew-live-run.sh`)

Part 3 of the plan drives ManaBrew's engine in-process (the `oracle` driver). A *live* game is
different: a real relay and a real room host, joined by a real client over the wire. This script
gives four-yay's `cmd/xclient -engine manabrew` something to join, and nothing else; the client
run, the codec and its tests live in four-yay.

- `scripts/manabrew-live-run.sh` builds `manabrew-server` (the relay) and `self-hosted-node` (the
  headless room host) from the tree named in [`MANABREW_LIVE_PIN`](../../../MANABREW_LIVE_PIN),
  starts both on free loopback ports with a random `MANABREW_SERVER_KEY` and
  `SELF_HOSTED_NODE_BOT_ENABLED=1` (the node hosts a room and seats ManaBrew's bot), prints the
  relay URL and room, and stops both by pid on exit.
- It shares the dev clone, cargo home and target dir of `scripts/manabrew-oracle-setup.sh`, in a
  separate detached worktree `live-<ref12>/`; the Forge card archive is the one
  `make manabrew-oracle-setup` builds. Everything is out of tree (ManaBrew is AGPL-3.0).
  Logs, pids and the key (`server.key`, mode 600) land in `<manabrew dir>/live/`.
- `scripts/manabrew-live-run.sh --selftest` builds, starts, checks with `ss -lptn` that the relay
  listens, that the node is up and has made its room, and stops.
- A client run, from four-yay:
  `MANABREW_SERVER_KEY=$(cat <dir>/live/server.key) go run ./cmd/xclient -engine manabrew -relay <url> -policy heuristic -games 100`.

- **The pin is upstream, not the oracle's.** `MANABREW_LIVE_REF` is upstream `dadee731`
  (`witchesofthehill/manabrew`, on `origin/main`), so a fresh box can fetch and build it.
  The oracle driver's `MANABREW_ORACLE_REF` (`8684462a6522`) exists only on the local `oracle`
  branch of the dev clone (the `adams-shaun/manabrew` fork is not public yet) and carries engine
  patches a live game does not want; `manabrew-server` and `self-hosted-node` are identical in
  the two trees. Moving the live pin to the fork is a two-line edit of `MANABREW_LIVE_PIN`, once
  the operator pushes it.
- Two deviations from the ticket text, both forced: logs live under `$MANABREW_ORACLE_DIR/live/`
  (not an in-repo `.oracle-build/`, which `manabrew-oracle-setup.sh` refuses for licence reasons),
  and the node is checked by an ESTABLISHED relay connection plus its "room created" log line,
  because `self-hosted-node` is a relay client with no listening socket.

Bumping the pin changes what every recorded live transcript is a statement about; bump it
deliberately, and re-record.
