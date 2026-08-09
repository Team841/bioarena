#!/usr/bin/env python3
"""Interactive serial console for configuring the field switch from the Pi.

Uses only the Python standard library. That is deliberate: the field Pis run older
Raspbian releases whose apt repositories have moved to archive.debian.org, so installing
screen or minicom is not always possible when you need a console at short notice.

Linux and macOS only -- it drives the terminal through termios, which Windows has no
equivalent of. Use PuTTY there, or run this from the Pi.

Usage:
    python3 console.py                       # /dev/ttyUSB0 at 9600 8N1
    python3 console.py /dev/ttyACM0          # Cisco USB console ports enumerate as ACM
    python3 console.py /dev/ttyUSB0 -b 115200
    python3 console.py --list                # show candidate serial devices

Press Ctrl-] to exit. Ctrl-C is passed through to the switch, not caught here, so it can
be used to interrupt IOS commands.
"""

import argparse
import glob
import os
import select
import sys

try:
    import termios
    import tty
except ImportError:  # pragma: no cover - platform guard
    sys.exit(
        "console.py needs a POSIX terminal (termios), so it does not run on Windows.\n"
        "  Run it from the Pi, which is on the field with the switch anyway:\n"
        "    scp docs/console.py <USER>@10.0.100.5:~/\n"
        "    ssh -t <USER>@10.0.100.5 'python3 ~/console.py'\n"
        "  Or use PuTTY on Windows: Connection type Serial, the COM port from Device\n"
        "  Manager, speed 9600, 8 data bits, 1 stop bit, no parity, no flow control."
    )

ESCAPE = b"\x1d"  # Ctrl-]

BAUD_RATES = {
    9600: termios.B9600,
    19200: termios.B19200,
    38400: termios.B38400,
    57600: termios.B57600,
    115200: termios.B115200,
}


def candidate_devices():
    """Serial devices most likely to be a console cable, most likely first."""
    return sorted(glob.glob("/dev/ttyUSB*")) + sorted(glob.glob("/dev/ttyACM*"))


def configure_port(fd, baud):
    """Put the serial port into raw 8N1 at the given speed, no flow control."""
    iflag, oflag, cflag, lflag, _, _, cc = termios.tcgetattr(fd)

    # Ignore modem control lines and enable the receiver, so the console works with
    # cables that do not wire the full set of handshake pins.
    cflag |= termios.CLOCAL | termios.CREAD
    cflag &= ~(termios.PARENB | termios.CSTOPB | termios.CSIZE)
    cflag |= termios.CS8

    # Hardware flow control off. Cisco console ports do not use it, and leaving it on
    # makes the port appear dead when the cable omits RTS/CTS.
    crtscts = getattr(termios, "CRTSCTS", 0)
    if crtscts:
        cflag &= ~crtscts

    # Fully raw: no translation, no echo, no signal generation on either side.
    iflag = 0
    oflag = 0
    lflag = 0

    cc = list(cc)
    cc[termios.VMIN] = 1
    cc[termios.VTIME] = 0

    speed = BAUD_RATES[baud]
    termios.tcsetattr(fd, termios.TCSANOW, [iflag, oflag, cflag, lflag, speed, speed, cc])


def open_port(device, baud):
    try:
        fd = os.open(device, os.O_RDWR | os.O_NOCTTY)
    except FileNotFoundError:
        found = candidate_devices()
        hint = "  Found: " + ", ".join(found) if found else "  No serial devices found."
        sys.exit(f"{device} does not exist.\n{hint}\n  Check the cable, then: dmesg | tail")
    except PermissionError:
        sys.exit(
            f"Permission denied opening {device}.\n"
            f"  Run with sudo, or add yourself to the dialout group and log in again:\n"
            f"    sudo usermod -aG dialout $USER"
        )

    try:
        configure_port(fd, baud)
    except Exception:
        os.close(fd)
        raise
    return fd


def run(device, baud):
    fd = open_port(device, baud)
    stdin_fd = sys.stdin.fileno()

    if not os.isatty(stdin_fd):
        os.close(fd)
        sys.exit("stdin is not a terminal; run this directly rather than through a pipe.")

    print(f"Connected to {device} at {baud} 8N1. Ctrl-] to exit.", flush=True)
    print("Press Enter a few times if nothing appears -- the console is silent until poked.", flush=True)

    saved = termios.tcgetattr(stdin_fd)
    try:
        tty.setraw(stdin_fd)
        while True:
            readable, _, _ = select.select([stdin_fd, fd], [], [])

            if stdin_fd in readable:
                data = os.read(stdin_fd, 1024)
                if not data or ESCAPE in data:
                    break
                os.write(fd, data)

            if fd in readable:
                data = os.read(fd, 1024)
                if data:
                    os.write(sys.stdout.fileno(), data)
    except OSError as err:
        # Most often the cable was unplugged mid-session. Report it on the restored
        # terminal rather than unwinding a traceback over a raw tty.
        error = err
    else:
        error = None
    finally:
        termios.tcsetattr(stdin_fd, termios.TCSADRAIN, saved)
        os.close(fd)
        print("\r\nDisconnected.", flush=True)

    if error is not None:
        sys.exit(f"Serial port error: {error}\n  Check the cable is still seated.")


def main():
    parser = argparse.ArgumentParser(
        description="Interactive serial console for the field switch.",
    )
    parser.add_argument(
        "device", nargs="?", default="/dev/ttyUSB0", help="serial device (default: /dev/ttyUSB0)"
    )
    parser.add_argument(
        "-b",
        "--baud",
        type=int,
        default=9600,
        choices=sorted(BAUD_RATES),
        help="baud rate (default: 9600, the Cisco console default)",
    )
    parser.add_argument(
        "--list", action="store_true", help="list candidate serial devices and exit"
    )
    args = parser.parse_args()

    if args.list:
        devices = candidate_devices()
        if devices:
            print("\n".join(devices))
        else:
            print("No serial devices found. Check the cable, then: dmesg | tail")
        return

    run(args.device, args.baud)


if __name__ == "__main__":
    main()
