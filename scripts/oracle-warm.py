#!/usr/bin/env python3
"""oracle-warm.py: replay one request file on a warm oracle JVM.

    oracle-warm.py --engine NAME --in IN --out OUT [--cwd DIR | --xmage-tests DIR] -- LAUNCH...

LAUNCH is the one-shot driver command without its IN OUT arguments (the scope
prefix, env assignments, java, flags, classpath and main class). The driver
must take `--serve SOCK IDLE_S MAX_ITEMS` (forge.oracle.WarmServer, the XMage
ScenarioReplay.serve). forge-oracle-run.sh and xmage-oracle-run.sh call this
when ORACLE_WARM=1 and the driver source has the serve mode.

A pool of ORACLE_WARM_SLOTS (2) workers per engine and launch command lives
under ORACLE_WARM_DIR (~/.cache/oracle-warm)/<engine>-<key>/s<k>/; the key
hashes LAUNCH, so a new pin, driver or classpath starts new workers and the old
ones exit after ORACLE_WARM_IDLE_S (600) idle seconds (their dirs are pruned
once dead and idle twice that). A worker also exits after ORACLE_WARM_MAX_ITEMS
(5000) rows. A caller claims a slot with flock, so
one worker serves one file at a time. A worker that does not answer "start"
within ORACLE_WARM_START_S (15) is busy with a file whose caller was killed (a
stage timeout): it is killed and replaced.

-Djava.io.tmpdir=... in LAUNCH (a per-batch directory) is dropped from the key
and the worker gets its own under its slot; --xmage-tests gives an XMage
worker its own working directory (symlinks into Mage.Tests and a private copy
of the card DB, as XMAGE_ORACLE_PRIVATE_DB does per run).

Exit 0: OUT written. Exit 3: no warm answer (the worker could not start, died
mid-file, or answered err); the caller runs the one-shot path instead, so a
warm failure never costs a row. Exit 2: usage.
"""
import argparse
import fcntl
import hashlib
import os
import shutil
import signal
import socket
import subprocess
import sys
import time

FALLBACK = 3


def log(msg):
    print("oracle-warm: " + msg, file=sys.stderr, flush=True)


def env_int(name, default):
    try:
        return int(os.environ.get(name, "") or default)
    except ValueError:
        return default


def worker_key(engine, launch, xmage_tests):
    parts = [engine, xmage_tests or ""] + [a for a in launch if not a.startswith("-Djava.io.tmpdir=")]
    return hashlib.sha256("\0".join(parts).encode()).hexdigest()[:12]


def claim(d, slots, block_on=None):
    """(slot number, open lock file) for a free slot; with none free, wait on slot block_on."""
    os.makedirs(d, exist_ok=True)
    for k in range(slots):
        f = open(os.path.join(d, "s%d.lock" % k), "w")
        try:
            fcntl.flock(f, fcntl.LOCK_EX | fcntl.LOCK_NB)
            return k, f
        except OSError:
            f.close()
    k = (os.getpid() if block_on is None else block_on) % slots
    f = open(os.path.join(d, "s%d.lock" % k), "w")
    fcntl.flock(f, fcntl.LOCK_EX)
    return k, f


def kill_worker(sd):
    """SIGKILL the slot's worker (its pid file, checked against its command line) and remove its socket."""
    sock = os.path.join(sd, "sock")
    try:
        with open(os.path.join(sd, "pid")) as f:
            pid = int(f.read().strip())
        with open("/proc/%d/cmdline" % pid, "rb") as f:
            if sock.encode() in f.read():
                os.kill(pid, signal.SIGKILL)
                for _ in range(100):
                    if not os.path.exists("/proc/%d" % pid):
                        break
                    time.sleep(0.05)
    except (OSError, ValueError):
        pass
    for n in ("sock", "pid"):
        try:
            os.unlink(os.path.join(sd, n))
        except OSError:
            pass


def prep_cwd(sd, xmage_tests, cwd):
    """The worker's working directory: cwd (else its slot dir), or (xmage_tests) symlinks into Mage.Tests plus a
    private card DB."""
    if not xmage_tests:
        return cwd or sd
    cwd = os.path.join(sd, "cwd")
    shutil.rmtree(cwd, ignore_errors=True)
    os.makedirs(os.path.join(cwd, "db"))
    for e in os.listdir(xmage_tests):
        if e != "db":
            os.symlink(os.path.join(xmage_tests, e), os.path.join(cwd, e))
    subprocess.run(["cp", "--reflink=auto", os.path.join(xmage_tests, "db", "cards.h2.mv.db"),
                    os.path.join(cwd, "db") + "/"], check=True)
    return cwd


def start_worker(sd, launch, xmage_tests, cwd, boot_s):
    kill_worker(sd)
    tmp = os.path.join(sd, "tmp")
    os.makedirs(tmp, exist_ok=True)
    argv = []
    for a in launch:
        if not a.startswith("-Djava.io.tmpdir="):
            argv.append(a)
        if os.path.basename(a) == "java":
            argv.append("-Djava.io.tmpdir=" + tmp)
    sock = os.path.join(sd, "sock")
    argv += ["--serve", sock, str(env_int("ORACLE_WARM_IDLE_S", 600)), str(env_int("ORACLE_WARM_MAX_ITEMS", 5000))]
    cwd = prep_cwd(sd, xmage_tests, cwd)
    with open(os.path.join(sd, "worker.log"), "ab") as lf:
        p = subprocess.Popen(argv, cwd=cwd, stdin=subprocess.DEVNULL, stdout=lf, stderr=lf, start_new_session=True)
    with open(os.path.join(sd, "pid"), "w") as f:
        f.write(str(p.pid))
    deadline = time.monotonic() + boot_s
    while time.monotonic() < deadline:
        if p.poll() is not None:
            log("worker exited rc=%s while starting (see %s/worker.log)" % (p.returncode, sd))
            return False
        if os.path.exists(sock):
            return True
        time.sleep(0.1)
    log("worker not listening after %ds" % boot_s)
    kill_worker(sd)
    return False


def ask(sd, inp, out, start_s):
    """'ok N' / 'err ...' from the slot's worker; None when it is not there; 'busy' when it never said start."""
    sock = os.path.join(sd, "sock")
    if not os.path.exists(sock):
        return None
    s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    try:
        s.connect(sock)
    except OSError:
        s.close()
        return None
    with s:
        f = s.makefile("rwb")
        f.write(("%s\t%s\n" % (inp, out)).encode())
        f.flush()
        s.settimeout(start_s)
        try:
            first = f.readline().decode().strip()
        except OSError:
            return "busy"
        if first != "start":
            return "busy" if not first else "err " + first
        s.settimeout(None)
        return f.readline().decode().strip() or "err worker closed the connection mid-file"


def prune(base, engine, keep, idle_s):
    """Remove this engine's other worker dirs (an older pin's or driver's) once none of their workers lives and
    they have been idle past twice IDLE_S: an XMage dir holds a private card DB copy per slot (~300 MB)."""
    try:
        names = os.listdir(base)
    except OSError:
        return
    for n in names:
        d = os.path.join(base, n)
        if not n.startswith(engine + "-") or d == keep or not os.path.isdir(d):
            continue
        live = False
        for sd in os.listdir(d):
            try:
                with open(os.path.join(d, sd, "pid")) as f:
                    live = live or os.path.exists("/proc/%d" % int(f.read().strip()))
            except (OSError, ValueError):
                pass
        try:
            old = time.time() - os.path.getmtime(d) > 2 * idle_s
        except OSError:
            continue
        if not live and old:
            shutil.rmtree(d, ignore_errors=True)


def main(argv):
    ap = argparse.ArgumentParser(add_help=False)
    ap.add_argument("--engine", required=True)
    ap.add_argument("--in", dest="inp", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--xmage-tests", default="")
    ap.add_argument("--cwd", default="")
    if "--" not in argv:
        log("usage: oracle-warm.py --engine NAME --in IN --out OUT [--xmage-tests DIR] -- LAUNCH...")
        return 2
    i = argv.index("--")
    a = ap.parse_args(argv[:i])
    launch = argv[i + 1:]
    if not launch:
        return 2
    inp, out = os.path.realpath(a.inp), os.path.realpath(a.out)
    base = os.environ.get("ORACLE_WARM_DIR") or os.path.join(os.path.expanduser("~"), ".cache", "oracle-warm")
    d = os.path.join(base, "%s-%s" % (a.engine, worker_key(a.engine, launch, a.xmage_tests + "|" + a.cwd)))
    slots = max(1, env_int("ORACLE_WARM_SLOTS", 2))
    k, lock = claim(d, slots)
    sd = os.path.join(d, "s%d" % k)
    os.makedirs(sd, exist_ok=True)
    if len(os.path.join(sd, "sock").encode()) > 100:   # AF_UNIX paths stop at 108 bytes
        log("socket path under %s is too long for a Unix socket; set a shorter ORACLE_WARM_DIR" % base)
        return FALLBACK
    start_s = env_int("ORACLE_WARM_START_S", 15)
    boot_s = env_int("ORACLE_WARM_BOOT_S", 300)
    prune(base, a.engine, d, env_int("ORACLE_WARM_IDLE_S", 600))
    with lock:
        for attempt in (0, 1):
            r = ask(sd, inp, out, start_s)
            if r is None or r == "busy":
                if r == "busy":
                    log("slot %d worker busy with an abandoned file; replacing it" % k)
                if attempt == 1 or not start_worker(sd, launch, a.xmage_tests, a.cwd, boot_s):
                    return FALLBACK
                continue
            if r.startswith("ok "):
                return 0
            log("slot %d: %s; falling back to a one-shot run" % (k, r))
            kill_worker(sd)
            return FALLBACK
    return FALLBACK


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
