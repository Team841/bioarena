#!/bin/sh
# Open the field controller full screen on this Pi's display, at startup.
#
# Installed by deploy-fms.sh into ~/.local/bin, and launched by the autostart entry
# alongside it. Runs in the desktop session as the login user, not as the service.

URL="http://localhost:8080"

# The desktop session is usually up before bioarena is: the service waits on the network,
# and the switch configuration it does at startup takes a few seconds more. Opening the
# browser first would land on a connection error and stay there, so wait for an answer
# rather than racing it.
while ! curl -sf -o /dev/null "$URL"; do
	sleep 2
done

# chromium-browser on Bookworm, chromium on Trixie.
BROWSER="$(command -v chromium-browser || command -v chromium)"
if [ -z "$BROWSER" ]; then
	echo "No chromium found. Install it with: sudo apt install chromium" >&2
	exit 1
fi

# --kiosk is the point. The rest suppress the things a browser does on a machine that gets
# powered off at the wall: the restore-pages bubble, update nagging, and the infobar that
# eats screen space on a field display nobody is going to click.
exec "$BROWSER" \
	--kiosk \
	--noerrdialogs \
	--disable-infobars \
	--disable-session-crashed-bubble \
	--check-for-update-interval=31536000 \
	"$URL"
