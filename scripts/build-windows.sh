#! /bin/sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT_DIR"

if ! command -v git >/dev/null 2>&1; then
	echo "error: git not found" >&2
	exit 1
fi

if command -v wails >/dev/null 2>&1; then
	WAILS=wails
else
	echo "wails not found: installing v2.15.0 into GOPATH/bin" >&2
	go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0
	WAILS="$(go env GOPATH)/bin/wails"
fi

COMMIT_TIME=$(git show -s --format=%cd --date=format:%Y-%m-%d-%H:%M HEAD)
COMMIT_SHORT=$(git rev-parse --short=7 HEAD)
APP_VERSION="v0.0.1-${COMMIT_TIME}-${COMMIT_SHORT}"

echo "building mdz-gui ${APP_VERSION}"

"$WAILS" build -ldflags "-X main.version=${APP_VERSION}"

echo "built: build/bin/mdz-gui.exe"
echo "version: ${APP_VERSION}"
