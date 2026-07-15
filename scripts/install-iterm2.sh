#!/bin/sh
# One-command installer for sm's iTerm2 native-window bridge (run on the Mac):
#   curl -fsSL https://raw.githubusercontent.com/dukechain2333/ai-sessions-manager/main/scripts/install-iterm2.sh | sh
set -e
DIR="$HOME/Library/Application Support/iTerm2/Scripts/AutoLaunch"
URL="https://raw.githubusercontent.com/dukechain2333/ai-sessions-manager/main/scripts/iterm2/sm_open_window.py"
mkdir -p "$DIR"
curl -fsSL "$URL" -o "$DIR/sm_open_window.py"
echo "Installed $DIR/sm_open_window.py"
echo
echo "Two manual steps in iTerm2:"
echo "  1. Settings > General > Magic > check 'Enable Python API'"
echo "  2. Scripts > AutoLaunch > sm_open_window.py (or restart iTerm2)."
echo "     First run offers to download the iTerm2 Python runtime - accept."
echo
echo "Then on the remote host, set in ~/.config/sm/config.json:"
echo '  "open_in": "window",'
echo '  "iterm2": { "ssh": "<how this Mac sshes there, e.g. myserver>" }'
