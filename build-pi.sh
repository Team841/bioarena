#!/usr/bin/env bash
# Cross-compile bioarena for Raspberry Pi 4.
# Run from the repo root on any machine with Go 1.22+ installed.
#
# Targets the 32-bit Raspberry Pi OS (armv7l / armhf), which is the default
# OS image for most Pi 4 installations.  If your Pi runs the 64-bit image
# (uname -m returns aarch64) change GOARCH to arm64 and remove GOARM.
#
# Output: bioarena  (single static binary, copy to the Pi and run)

set -euo pipefail

OUTPUT="bioarena"

echo "Building for linux/arm (armv7 / 32-bit Raspberry Pi OS)..."
GOOS=linux GOARCH=arm GOARM=7 go build -o "$OUTPUT" .

echo "Done: $OUTPUT"
echo ""
echo "Deploy steps:"
echo "  1. Copy the binary and static assets to the Pi:"
echo "       scp $OUTPUT pi@<PI_IP>:~/bioarena/"
echo "       scp -r static templates font schedules audio pi@<PI_IP>:~/bioarena/"
echo ""
echo "  2. On the Pi, make the binary executable:"
echo "       chmod +x ~/bioarena/$OUTPUT"
echo ""
echo "  3. Install the systemd service so it starts on boot:"
echo "       scp bioarena.service pi@<PI_IP>:~/"
echo "       # then on the Pi:"
echo "       sudo mv ~/bioarena.service /etc/systemd/system/"
echo "       sudo systemctl daemon-reload"
echo "       sudo systemctl enable bioarena"
echo "       sudo systemctl start bioarena"
echo ""
echo "  4. Access the web UI at http://<PI_IP>:8080"
echo ""
echo "Useful service commands (run on the Pi):"
echo "  sudo systemctl status bioarena   # check it's running"
echo "  sudo journalctl -u bioarena -f   # tail live logs"
echo "  sudo systemctl restart bioarena  # restart after a new deploy"
echo ""
echo "Network note:"
echo "  The service assigns 10.0.100.5/24 to eth0 automatically on start."
echo "  Driver stations connect to that address on ports 1750/1120/1121."
