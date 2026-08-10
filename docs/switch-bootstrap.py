#!/usr/bin/env python3
"""Bring a factory or reset Catalyst 3560-CX to the point bioarena can configure it.

Bioarena applies the field's standing configuration itself -- VLANs, station and trunk
ports, routing -- but it can only do that over Telnet, and Telnet needs an address and a
password that do not exist on a switch out of the box. That bootstrap is console-only by
definition. This script does it, so nobody composes IOS by hand:

    python3 switch-bootstrap.py --password fieldpassword

Sets the hostname, the management address, the enable and VTY passwords, enables Telnet,
and points the boot loader at the installed image so the switch stops stopping at the
"switch:" prompt. Then saves.

Uses only the Python standard library, and Linux or macOS only, for the same reasons as
console.py -- run it from the Pi.

Everything here is idempotent: running it twice changes nothing the second time.
"""

import argparse
import os
import re
import select
import sys
import time

try:
    import termios
except ImportError:  # pragma: no cover - platform guard
    sys.exit(
        "switch-bootstrap.py needs a POSIX terminal (termios), so it does not run on\n"
        "Windows. Run it from the Pi, which is on the field with the switch anyway."
    )

DEFAULT_ADDRESS = "10.0.100.3"
DEFAULT_MASK = "255.255.255.0"
DEFAULT_HOSTNAME = "FieldSwitch"
READ_TIMEOUT_SEC = 5


def open_port(device, baud=9600):
    """Open the console at 9600 8N1, no flow control -- Cisco's console defaults."""
    fd = os.open(device, os.O_RDWR | os.O_NOCTTY)
    iflag, oflag, cflag, lflag, _, _, cc = termios.tcgetattr(fd)
    cflag |= termios.CLOCAL | termios.CREAD
    cflag &= ~(termios.PARENB | termios.CSTOPB | termios.CSIZE)
    cflag |= termios.CS8
    crtscts = getattr(termios, "CRTSCTS", 0)
    if crtscts:
        cflag &= ~crtscts
    iflag = oflag = lflag = 0
    cc = list(cc)
    cc[termios.VMIN] = 0
    cc[termios.VTIME] = 0
    speed = getattr(termios, "B%d" % baud)
    termios.tcsetattr(fd, termios.TCSANOW, [iflag, oflag, cflag, lflag, speed, speed, cc])
    return fd


def read_until_idle(fd, idle_sec=0.6, timeout_sec=READ_TIMEOUT_SEC):
    """Collect output until the switch stops talking.

    Waiting for a specific prompt is unreliable here: the prompt changes as the
    configuration proceeds, an unconfigured switch may be offering its setup dialog, and
    "write memory" answers with progress dots. Idleness is the one signal that means the
    same thing in every state.
    """
    output = ""
    deadline = time.time() + timeout_sec
    last_data = time.time()
    while time.time() < deadline:
        readable, _, _ = select.select([fd], [], [], 0.1)
        if readable:
            chunk = os.read(fd, 4096)
            if chunk:
                output += chunk.decode("utf-8", errors="replace")
                last_data = time.time()
                continue
        if time.time() - last_data >= idle_sec:
            break
    return output


def send(fd, line, echo=True):
    os.write(fd, (line + "\r").encode())
    output = read_until_idle(fd)
    if echo and line:
        print("  %s" % line)
    return output


def find_boot_image(fd):
    """Locate the IOS image so the switch boots on its own.

    A switch whose BOOT variable is unset stops at the boot loader on every power cycle,
    which reads as a dead switch on the morning of a practice session.
    """
    output = send(fd, "dir /recursive flash:", echo=False)
    images = re.findall(r"\S+\.bin", output)
    if not images:
        return None
    image = images[0]
    directories = re.findall(r"\s(\S*%s)\s" % re.escape(image), output)
    return directories[0] if directories else image


def bootstrap(fd, args):
    print("Waking the console...")
    send(fd, "", echo=False)
    output = send(fd, "", echo=False)

    # A switch with no configuration offers its setup dialog first; decline it, since
    # everything it would ask is set below.
    if "initial configuration dialog" in output:
        print("Declining the setup dialog.")
        send(fd, "no", echo=False)
        send(fd, "", echo=False)

    print("Entering privileged mode...")
    output = send(fd, "enable", echo=False)
    if "Password:" in output:
        send(fd, args.password, echo=False)

    image = find_boot_image(fd)
    if image:
        print("Found IOS image: %s" % image)
    else:
        print("WARNING: no .bin image found in flash; leaving the boot setting alone.")
        print("         The switch may stop at the 'switch:' prompt on the next reboot.")

    print("Applying bootstrap configuration:")
    send(fd, "configure terminal", echo=False)
    for line in [
        "hostname %s" % args.hostname,
        "interface Vlan1",
        "ip address %s %s" % (args.address, args.mask),
        "no shutdown",
        "exit",
        "enable secret %s" % args.password,
        "line vty 0 4",
        "password %s" % args.password,
        "login",
        "transport input telnet",
        "exit",
        "line vty 5 15",
        "transport input none",
        "exit",
        "service password-encryption",
    ] + (["boot system flash:%s" % image] if image else []):
        # The passwords are the point of the exercise, so they are not echoed.
        send(fd, line, echo="password" not in line and "secret" not in line)
    send(fd, "end", echo=False)

    print("Saving...")
    send(fd, "write memory", echo=False)

    print("")
    print("Done. The switch answers Telnet at %s." % args.address)
    print("Enter that address and the password under Arena > Settings; bioarena applies")
    print("the VLANs, station ports, trunks and routing itself on the next match load.")


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--device", default="/dev/ttyUSB0", help="serial device (default: /dev/ttyUSB0)")
    parser.add_argument("--address", default=DEFAULT_ADDRESS, help="management address (default: %s)" % DEFAULT_ADDRESS)
    parser.add_argument("--mask", default=DEFAULT_MASK, help="management subnet mask")
    parser.add_argument("--hostname", default=DEFAULT_HOSTNAME, help="switch hostname, ideally naming the site")
    parser.add_argument(
        "--password",
        required=True,
        help="enable and VTY password. Bioarena sends one password for both, so they must match.",
    )
    args = parser.parse_args()

    try:
        fd = open_port(args.device)
    except FileNotFoundError:
        sys.exit("%s does not exist. Check the console cable, then: dmesg | tail" % args.device)
    except PermissionError:
        sys.exit(
            "Permission denied opening %s.\n"
            "  Run with sudo, or add yourself to the dialout group and log in again." % args.device
        )

    try:
        bootstrap(fd, args)
    finally:
        os.close(fd)


if __name__ == "__main__":
    main()
