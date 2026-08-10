#!/usr/bin/env bash
# Build and push bioarena to a field Pi.
#
# The common case is a code change with no asset change, so that is the default and it
# copies one file:
#
#   ./deploy-pi.sh
#
# Add --assets when static/ or templates/ changed, --service when bioarena.service did.
# Neither is guessed: copying assets every time is most of the wall clock, and copying the
# service file needs a sudo move and a daemon-reload that are wasted when it has not
# changed.
#
#   ./deploy-pi.sh --assets
#   ./deploy-pi.sh --assets --service
#   ./deploy-pi.sh --target 10.2.100.5 --user admin
#   ./deploy-pi.sh --panel 10.0.100.11        # an e-stop panel Pi instead
#
# deploy-pi.ps1 is the same thing for PowerShell. Set up key authentication first or this
# asks for a password three times per deploy -- see "Faster deploys" in the README.

set -euo pipefail

TARGET="10.0.100.5"
USER_NAME="admin"
PANEL=""
ARCH="arm64"
ASSETS=false
SERVICE=false
SKIP_BUILD=false

while [ $# -gt 0 ]; do
	case "$1" in
	--target) TARGET="$2"; shift 2 ;;
	--user) USER_NAME="$2"; shift 2 ;;
	--panel) PANEL="$2"; shift 2 ;;
	--arch) ARCH="$2"; shift 2 ;;
	--assets) ASSETS=true; shift ;;
	--service) SERVICE=true; shift ;;
	--skip-build) SKIP_BUILD=true; shift ;;
	-h|--help) sed -n '2,20p' "$0"; exit 0 ;;
	*) echo "Unknown option: $1" >&2; exit 2 ;;
	esac
done

if [ -n "$PANEL" ]; then
	REMOTE="$USER_NAME@$PANEL"
	BINARY="estop-panel-pi"
	DIRECTORY="/opt/estop-panel"
	UNIT="estop-panel"
	PACKAGE="./cmd/estop-panel"
	UNIT_FILE="cmd/estop-panel/estop-panel.service"
else
	REMOTE="$USER_NAME@$TARGET"
	BINARY="bioarena-pi"
	DIRECTORY="/opt/bioarena"
	UNIT="bioarena"
	PACKAGE="."
	UNIT_FILE="bioarena.service"
fi

started=$(date +%s)

step() {
	echo "==> $1"
}

if [ "$SKIP_BUILD" = false ]; then
	step "Building $BINARY for linux/$ARCH"
	export GOOS=linux GOARCH="$ARCH"
	if [ "$ARCH" = "arm" ]; then export GOARM=7; else unset GOARM; fi
	go build -o "$BINARY" "$PACKAGE"
fi

# Linux refuses to overwrite a running executable, and scp reports that as a bare
# "Failure", so the service comes down before the copy rather than after.
step "Stopping $UNIT"
ssh "$REMOTE" "sudo systemctl stop $UNIT"

step "Copying $BINARY"
scp "$BINARY" "$REMOTE:$DIRECTORY/"

if [ "$ASSETS" = true ]; then
	step "Copying static and templates"
	scp -r static templates "$REMOTE:$DIRECTORY/"
fi

if [ "$SERVICE" = true ]; then
	step "Installing $UNIT.service"
	scp "$UNIT_FILE" "$REMOTE:~/"
	ssh "$REMOTE" "sudo mv ~/$UNIT.service /etc/systemd/system/ && sudo systemctl daemon-reload"
fi

# Ownership is deliberately left alone: the service reads these files and writes only what
# it creates itself. Chowning them to the service account breaks the next deploy, because
# scp overwrites by opening the existing file for writing.
step "Starting $UNIT"
ssh "$REMOTE" "chmod +x $DIRECTORY/$BINARY && sudo systemctl start $UNIT"

echo ""
ssh "$REMOTE" "systemctl is-active $UNIT && systemctl show -p ActiveEnterTimestamp --value $UNIT"

echo ""
echo "Deployed in $(($(date +%s) - started))s. Watch it with:"
echo "  ssh $REMOTE 'journalctl -u $UNIT -f'"
