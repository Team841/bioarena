#!/usr/bin/env python3
"""Unattended stand-in for the Hub FUEL counter, for running continuously.

Speaks the counter protocol in docs/spec-hub-fuel-counter-feed.md, section 4, from a
Windows box or anything else with Python 3. Two modes:

  score   FUEL scored into a random HUB at a random interval -- steady fake scoring, so
          the match panel always has something moving.
  idle    No scoring. Heartbeats only, so the counter shows ONLINE with zero counts.

Run it until you stop it:

  python docs\\hub_counter_feed.py --target 10.0.100.5 --mode score
  python docs\\hub_counter_feed.py --target 10.0.100.5 --mode idle

--target is the FMS Pi as seen from this machine: 10.0.100.5 when this box is wired into
the field switch, otherwise whatever address the Pi has on the network the two share
(`hostname -I` on the Pi). Bioarena only accepts datagrams from the address set as
Counter address under Arena > Settings, so that has to be this machine's address.

It never stops on its own. Bioarena being down, restarting or unreachable is reported in
the status line and the feed carries on; Ctrl+C is the only way out. Standard library
only, so there is nothing to install.
"""

import argparse
import json
import random
import socket
import sys
import time

HEARTBEAT_S = 0.1  # spec 4.3: send every 100 ms while the counts are unchanged
STATUS_EVERY_S = 5.0  # how often the status line prints
REPLY_FRESH_S = 2.0  # a reply older than this means the FMS link is down


def parse_args():
    parser = argparse.ArgumentParser(
        description="Continuous Hub FUEL counter feed: steady fake scoring, or heartbeats only.",
    )
    parser.add_argument("--target", default="10.0.100.5",
                        help="FMS Pi address as seen from this machine (default 10.0.100.5)")
    parser.add_argument("--port", type=int, default=8411,
                        help="bioarena's Listen port (UDP) from Arena > Settings (default 8411)")
    parser.add_argument("--mode", choices=["score", "idle"], default="score",
                        help="score: FUEL at random intervals; idle: heartbeats only (default score)")
    parser.add_argument("--min-interval", type=float, default=0.5,
                        help="score mode: shortest gap between FUEL, seconds (default 0.5)")
    parser.add_argument("--max-interval", type=float, default=3.0,
                        help="score mode: longest gap between FUEL, seconds (default 3.0)")
    parser.add_argument("--hubs", choices=["both", "red", "blue"], default="both",
                        help="score mode: which HUBs score; one for a half field (default both)")
    args = parser.parse_args()
    if not 0 < args.min_interval <= args.max_interval:
        parser.error("need 0 < --min-interval <= --max-interval")
    return args


def open_socket():
    """One socket for the life of the feed, so bioarena's replies come back to it (spec 4.3)."""
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    # Windows turns the ICMP "port unreachable" a send provokes while bioarena is down into
    # an error on the next read from this socket. Switch that off where the platform allows;
    # the receive loop also tolerates it, for when it does not.
    if hasattr(socket, "SIO_UDP_CONNRESET"):
        sock.ioctl(socket.SIO_UDP_CONNRESET, False)
    return sock


def main():
    args = parse_args()
    target = (args.target, args.port)
    scoring_hubs = ["red", "blue"] if args.hubs == "both" else [args.hubs]

    # New on every start (spec 4.5), so bioarena sees a restart rather than a regression.
    session = f"{random.getrandbits(32):08x}"
    sock = open_socket()
    counts = {"red": 0, "blue": 0}
    seq = 0
    last_increment = None
    last_sent = 0.0
    next_score = time.monotonic() + random.uniform(args.min_interval, args.max_interval)

    sent = send_errors = replies = 0
    last_reply = None
    last_reply_at = None
    last_error = ""
    next_status = time.monotonic() + STATUS_EVERY_S

    print(f"Hub counter feed: mode {args.mode}, session {session}, sending to "
          f"{target[0]}:{target[1]}. Ctrl+C to stop.", flush=True)
    if args.mode == "score":
        print(f"Scoring {' and '.join(scoring_hubs)} every "
              f"{args.min_interval:g}-{args.max_interval:g} s.", flush=True)

    def send():
        nonlocal seq, last_sent, sent, send_errors, last_error, sock
        seq += 1
        message = {"v": 1, "session": session, "seq": seq, **counts}
        if last_increment is not None:
            message["age_ms"] = int((time.monotonic() - last_increment) * 1000)
        message["info"] = f"hub_counter_feed {args.mode}"
        last_sent = time.monotonic()
        try:
            sock.sendto(json.dumps(message, separators=(",", ":")).encode(), target)
            sent += 1
        except OSError as err:
            # No route, network down, adapter gone. Keep the session and the counts, and
            # start over with a fresh socket in case this one is past saving.
            send_errors += 1
            last_error = str(err)
            sock.close()
            sock = open_socket()

    try:
        while True:
            now = time.monotonic()
            if args.mode == "score" and now >= next_score:
                counts[random.choice(scoring_hubs)] += 1
                last_increment = now
                send()  # on change: immediately (spec 4.3)
                next_score = now + random.uniform(args.min_interval, args.max_interval)
            elif now - last_sent >= HEARTBEAT_S:
                send()

            # Wait for a reply until the next send is due.
            due = last_sent + HEARTBEAT_S
            if args.mode == "score":
                due = min(due, next_score)
            sock.settimeout(max(0.001, due - time.monotonic()))
            try:
                data, _ = sock.recvfrom(2048)
                last_reply = json.loads(data)
                last_reply_at = time.monotonic()
                replies += 1
            except (socket.timeout, ConnectionResetError, ValueError):
                pass  # no reply yet, bioarena down, or a reply we cannot read: carry on
            except OSError as err:
                last_error = str(err)

            if time.monotonic() >= next_status:
                next_status += STATUS_EVERY_S
                print(status_line(counts, sent, send_errors, replies,
                                  last_reply, last_reply_at, last_error), flush=True)
                last_error = ""
    except KeyboardInterrupt:
        print(f"\nStopped. Sent {sent}, final counts red {counts['red']} blue {counts['blue']}.")
        return 0


def status_line(counts, sent, send_errors, replies, last_reply, last_reply_at, last_error):
    stamp = time.strftime("%H:%M:%S")
    line = f"{stamp}  red {counts['red']:>5}  blue {counts['blue']:>5}  sent {sent}"
    if send_errors:
        line += f"  send errors {send_errors}"
    if last_reply_at is not None and time.monotonic() - last_reply_at < REPLY_FRESH_S:
        line += (f"  | FMS link OK, {last_reply.get('match_state', '?')}"
                 f" {last_reply.get('shift', '')}".rstrip())
        credited = last_reply.get("credited")
        if isinstance(credited, dict):
            line += f", credited red {credited.get('red', 0)} blue {credited.get('blue', 0)}"
    else:
        line += ("  | no reply from bioarena: is it running, is --target right, and is "
                 "Counter address this machine's address?")
    if last_error:
        line += f"  | last error: {last_error}"
    return line


if __name__ == "__main__":
    sys.exit(main())
