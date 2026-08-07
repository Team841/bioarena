#!/usr/bin/env bash
# Cross-compile bioarena for Raspberry Pi 4.
# Run from the repo root on any machine with Go 1.22+ installed.
#
# Targets 64-bit Raspberry Pi OS (aarch64), which is what Trixie and Bookworm
# install by default.  Confirm with "uname -m" on the Pi:
#
#   aarch64   ARCH=arm64                     (the default below)
#   armv7l    ARCH=arm  with GOARM=7 set     (32-bit images, including Buster)
#
# Override without editing this file:
#   ARCH=arm GOARM=7 ./build-pi.sh
#
# A binary built for the wrong one does not fail usefully -- the kernel refuses
# to run it and reports "cannot execute binary file: Exec format error".
#
# Output: bioarena-pi  estop-panel-pi  (Linux/ARM binaries, copy to the Pi)
# Note: outputs use a "-pi" suffix so they never shadow a local Windows build.

set -euo pipefail

OUTPUT="bioarena-pi"
PANEL_OUTPUT="estop-panel-pi"

ARCH="${ARCH:-arm64}"
GOARM="${GOARM:-}"
export GOOS=linux GOARCH="$ARCH"
if [ "$ARCH" = "arm" ]; then
	export GOARM="${GOARM:-7}"
	DESCRIPTION="linux/arm (armv7 / 32-bit Raspberry Pi OS)"
else
	DESCRIPTION="linux/arm64 (aarch64 / 64-bit Raspberry Pi OS)"
fi

echo "Building bioarena for $DESCRIPTION..."
go build -o "$OUTPUT" .

echo "Building estop-panel for $DESCRIPTION..."
go build -o "$PANEL_OUTPUT" ./cmd/estop-panel

echo "Done: $OUTPUT  $PANEL_OUTPUT"
echo ""
echo "Deploy steps — main field controller Pi:"
echo "  0. If bioarena is already installed, stop it first. Linux refuses to overwrite a"
echo "     running executable, and scp reports that as a bare \"Failure\":"
echo "       ssh pi@<PI_IP> 'sudo systemctl stop bioarena'"
echo ""
echo "  1. Copy the binary, static assets, and service file to the Pi:"
echo "       scp $OUTPUT pi@<PI_IP>:~/bioarena/"
echo "       scp -r static templates pi@<PI_IP>:~/bioarena/"
echo "       scp bioarena.service pi@<PI_IP>:~/"
echo "     Keep the $OUTPUT filename — bioarena.service runs it by that path."
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
echo "Deploy steps — e-stop panel Pi (repeat for red and blue):"
echo "  0. If estop-panel is already installed, stop it first (same reason as above):"
echo "       ssh pi@<PANEL_PI_IP> 'sudo systemctl stop estop-panel'"
echo ""
echo "  1. Copy the panel binary and config to the panel Pi:"
echo "       scp $PANEL_OUTPUT pi@<PANEL_PI_IP>:~/estop-panel/estop-panel"
echo "       scp estop-panel.yaml pi@<PANEL_PI_IP>:~/estop-panel/"
echo ""
echo "  2. Make it executable:"
echo "       chmod +x ~/estop-panel/estop-panel"
echo ""
echo "  3. Install the systemd service (edit IP in service file first):"
echo "       scp cmd/estop-panel/estop-panel.service pi@<PANEL_PI_IP>:~/"
echo "       # then on the panel Pi:"
echo "       sudo mv ~/estop-panel.service /etc/systemd/system/"
echo "       sudo systemctl daemon-reload"
echo "       sudo systemctl enable estop-panel"
echo "       sudo systemctl start estop-panel"
echo ""
echo "Useful service commands (run on any Pi):"
echo "  sudo systemctl status bioarena   # check it's running"
echo "  sudo journalctl -u bioarena -f   # tail live logs"
echo "  sudo systemctl restart bioarena  # restart after a new deploy"
echo ""
echo "Trixie/Bookworm note (team_network_driver: local):"
echo "  NetworkManager claims the VLAN subinterfaces and can strip their addresses."
echo "  Install the drop-in once per field Pi:"
echo "       scp docs/99-bioarena-unmanaged.conf pi@<PI_IP>:~/"
echo "       # then on the Pi:"
echo "       sudo mv ~/99-bioarena-unmanaged.conf /etc/NetworkManager/conf.d/"
echo "       sudo systemctl reload NetworkManager"
echo ""
echo "Network note:"
echo "  Main Pi:        10.0.100.5/24  (eth0, set by bioarena.service)"
echo "  Red panel Pi:   10.0.100.11/24 (eth0, set by estop-panel.service)"
echo "  Blue panel Pi:  10.0.100.12/24 (eth0, set by estop-panel.service)"
