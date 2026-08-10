#!/usr/bin/env bash
# Deploy bioarena to a field controller Pi.
#
#   ./deploy-fms.sh 10.0.100.5
#   ./deploy-fms.sh 10.0.100.5 admin      # if you log in as something other than admin
#
# Does everything, every time: builds, creates the service account if this Pi has never
# been deployed to, copies the binary and the web assets, installs the service, starts it,
# and checks that it came up. Nothing is optional and nothing is remembered between runs,
# because a deploy that needs you to know which flag to pass is a deploy that eventually
# goes out missing a file.
#
# Safe to run repeatedly. Safe to run on a Pi that has never seen bioarena.

set -euo pipefail

TARGET="${1:-}"
LOGIN="${2:-admin}"

if [ -z "$TARGET" ]; then
	echo "Usage: ./deploy-fms.sh <pi-address> [login-user]" >&2
	echo "" >&2
	echo "Example: ./deploy-fms.sh 10.0.100.5" >&2
	echo "" >&2
	echo "The address is the Pi's, not the switch's or the access point's. On a field" >&2
	echo "that is 10.X.100.5; on the bench it is whatever the Pi is on." >&2
	exit 2
fi

REMOTE="$LOGIN@$TARGET"
STAGING=".bioarena-deploy"
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

announce "Building for the Pi"
GOOS=linux GOARCH=arm64 go build -o bioarena-pi .
echo "      bioarena-pi built"

announce "Copying to $TARGET"
# Into the login user's home first, which needs no privileges. Everything that does is
# done in one go below, so the Pi asks for a sudo password once rather than at every step.
ssh -o ConnectTimeout=10 "$REMOTE" "rm -rf ~/$STAGING && mkdir -p ~/$STAGING"
scp -q bioarena-pi bioarena.service "$REMOTE:~/$STAGING/"
scp -qr static templates "$REMOTE:~/$STAGING/"
echo "      binary, service file, static, templates"

announce "Installing (the Pi may ask for your password)"
# -t so sudo has a terminal to prompt on: without it sudo refuses with "a terminal is
# required to read the password", which reads like a bug in the deploy rather than a
# missing tty.
ssh -t "$REMOTE" "
	set -e
	# Idempotent: creates the service account the first time, does nothing after. It is a
	# system user with no login, so a field controller does not depend on which username
	# the SD card was flashed with.
	id bioarena >/dev/null 2>&1 || sudo useradd --system --home-dir /opt/bioarena --shell /usr/sbin/nologin bioarena
	sudo mkdir -p /opt/bioarena
	sudo chown bioarena:bioarena /opt/bioarena

	# Stopped before the binary is replaced: Linux refuses to overwrite a running
	# executable, and the error reads like a permissions problem.
	sudo systemctl stop bioarena 2>/dev/null || true

	sudo install -o bioarena -g bioarena -m 755 ~/$STAGING/bioarena-pi /opt/bioarena/bioarena-pi
	sudo cp -r ~/$STAGING/static ~/$STAGING/templates /opt/bioarena/
	sudo chown -R bioarena:bioarena /opt/bioarena/static /opt/bioarena/templates
	sudo cp ~/$STAGING/bioarena.service /etc/systemd/system/bioarena.service
	sudo systemctl daemon-reload
	sudo systemctl enable bioarena >/dev/null 2>&1
	sudo systemctl start bioarena
	rm -rf ~/$STAGING
"
echo "      installed to /opt/bioarena"

announce "Checking it stayed up"
sleep 2
if ! ssh "$REMOTE" "systemctl is-active --quiet bioarena"; then
	echo "" >&2
	echo "DEPLOY FAILED: the service started and then stopped." >&2
	echo "" >&2
	ssh "$REMOTE" "journalctl -u bioarena -n 20 --no-pager" >&2 || true
	exit 1
fi

trap - ERR
echo "      running"
echo ""
echo "Done. Open the field at http://$TARGET:8080"
echo ""
echo "If something looks wrong, watch what it is doing:"
echo "  ssh $REMOTE 'journalctl -u bioarena -f'"
