#!/usr/bin/env bash
# Deploy the e-stop panel service to an alliance panel Pi.
#
#   ./deploy-panel.sh 10.0.100.11 red
#   ./deploy-panel.sh 10.0.100.12 blue
#   ./deploy-panel.sh 10.0.100.11 red admin   # if you log in as something other than admin
#
# The alliance is required because a panel Pi is not interchangeable: it takes the static
# address the field controller expects to poll for that alliance, and the wrong one gives
# two panels the same address and a field whose e-stops answer for the wrong side.
#
# Does everything, every time: builds, creates the service account if this Pi has never
# been deployed to, puts it in the gpio group, writes the alliance's address into the
# service file, installs, starts, and checks it came up.
#
# Safe to run repeatedly. Safe to run on a Pi that has never seen the panel service.

set -euo pipefail

TARGET="${1:-}"
ALLIANCE="$(echo "${2:-}" | tr '[:upper:]' '[:lower:]')"
LOGIN="${3:-admin}"

usage() {
	echo "Usage: ./deploy-panel.sh <panel-pi-address> <red|blue> [login-user]" >&2
	echo "" >&2
	echo "Examples: ./deploy-panel.sh 10.0.100.11 red" >&2
	echo "          ./deploy-panel.sh 10.0.100.12 blue" >&2
	echo "" >&2
	echo "Red panels take 10.0.100.11, blue panels 10.0.100.12. Those are the addresses" >&2
	echo "the field controller polls, set under Arena > Settings." >&2
	exit 2
}

[ -n "$TARGET" ] || usage
case "$ALLIANCE" in
red) PANEL_ADDRESS="10.0.100.11" ;;
blue) PANEL_ADDRESS="10.0.100.12" ;;
*) usage ;;
esac

REMOTE="$LOGIN@$TARGET"
STAGING=".bioarena-deploy"
SERVICE_FILE="$(mktemp)"
trap 'rm -f "$SERVICE_FILE"' EXIT
step=0

announce() {
	step=$((step + 1))
	echo ""
	echo "[$step/4] $1"
}

fail() {
	echo "" >&2
	echo "DEPLOY FAILED: $1" >&2
	echo "" >&2
	echo "Fix the cause and run this again -- repeating it is safe." >&2
	exit 1
}

trap 'fail "step $step did not complete"' ERR

announce "Building for the $ALLIANCE panel"
GOOS=linux GOARCH=arm64 go build -o estop-panel-pi ./cmd/estop-panel
# The address lives in the service file, which ships with the red one. Editing it by hand
# per panel is the step people forget, and forgetting it puts two panels on one address.
sed "s#ip addr add 10\.0\.100\.[0-9]\+/24#ip addr add $PANEL_ADDRESS/24#" \
	cmd/estop-panel/estop-panel.service >"$SERVICE_FILE"
grep -q "$PANEL_ADDRESS/24" "$SERVICE_FILE" || fail "could not set the address in estop-panel.service"
echo "      estop-panel-pi built, service file set to $PANEL_ADDRESS"

announce "Copying to $TARGET"
# Into the login user's home first, which needs no privileges. Everything that does is done
# in one go below, so the Pi asks for a sudo password once rather than at every step.
ssh -o ConnectTimeout=10 "$REMOTE" "rm -rf ~/$STAGING && mkdir -p ~/$STAGING"
scp -q estop-panel-pi "$REMOTE:~/$STAGING/estop-panel"
scp -q "$SERVICE_FILE" "$REMOTE:~/$STAGING/estop-panel.service"
if [ -f estop-panel.yaml ]; then
	scp -q estop-panel.yaml "$REMOTE:~/$STAGING/"
fi
echo "      binary and service file"

announce "Installing (the Pi may ask for your password)"
# -t so sudo has a terminal to prompt on: without it sudo refuses with "a terminal is
# required to read the password", which reads like a bug in the deploy rather than a
# missing tty.
ssh -t "$REMOTE" "
	set -e
	id bioarena >/dev/null 2>&1 || sudo useradd --system --home-dir /opt/estop-panel --shell /usr/sbin/nologin bioarena
	# The gpio group is the one that bites: without it the panel starts, reports that it
	# cannot open the GPIO chip, and then reports no stops at all -- a field that looks
	# healthy with e-stops that do nothing.
	sudo usermod -aG gpio bioarena
	sudo mkdir -p /opt/estop-panel
	sudo chown bioarena:bioarena /opt/estop-panel

	sudo systemctl stop estop-panel 2>/dev/null || true

	sudo install -o bioarena -g bioarena -m 755 ~/$STAGING/estop-panel /opt/estop-panel/estop-panel
	if [ -f ~/$STAGING/estop-panel.yaml ]; then
		sudo install -o bioarena -g bioarena -m 644 ~/$STAGING/estop-panel.yaml /opt/estop-panel/estop-panel.yaml
	fi
	sudo cp ~/$STAGING/estop-panel.service /etc/systemd/system/estop-panel.service
	sudo systemctl daemon-reload
	sudo systemctl enable estop-panel >/dev/null 2>&1
	sudo systemctl start estop-panel
	rm -rf ~/$STAGING
"
echo "      installed to /opt/estop-panel"

announce "Checking it stayed up"
sleep 2
if ! ssh "$REMOTE" "systemctl is-active --quiet estop-panel"; then
	echo "" >&2
	echo "DEPLOY FAILED: the panel service started and then stopped." >&2
	echo "" >&2
	ssh "$REMOTE" "journalctl -u estop-panel -n 20 --no-pager" >&2 || true
	exit 1
fi

trap - ERR
echo "      running at $PANEL_ADDRESS"
echo ""
echo "Done. Tell the field controller about it under Arena > Settings:"
if [ "$ALLIANCE" = "red" ]; then
	echo "  Red E-Stop Panel Address: http://$PANEL_ADDRESS:8765"
else
	echo "  Blue E-Stop Panel Address: http://$PANEL_ADDRESS:8765"
fi
echo ""
echo "Then press the button and watch the field react. If it does not:"
echo "  ssh $REMOTE 'journalctl -u estop-panel -f'"
