#! /bin/sh
set -eu

REPO="${GH_REPO:-hatolife/mdz-gui}"
STAMP="${MDZ_BUILD_STAMP:-$(date -u +%Y%m%d-%H%M%S)}"
TAG="${MDZ_RELEASE_TAG:-build-${STAMP}}"
ASSET_NAME="mdz-gui-windows-amd64-${STAMP}.zip"
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
TMP_DIR=""

cleanup() {
	if [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ]; then
		rm -rf "$TMP_DIR"
	fi
}

fail() {
	echo "error: $*" >&2
	exit 1
}

require_command() {
	command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

publish_with_api() {
	token=${GH_TOKEN:-${GITHUB_TOKEN:-}}
	[ -n "$token" ] || fail "GitHub CLI is not installed and GH_TOKEN/GITHUB_TOKEN is not set"
	require_command curl
	require_command jq

	body=$(cat "$TMP_DIR/release-notes.md")
	payload=$(jq -n \
		--arg tag "$TAG" \
		--arg target "$COMMIT" \
		--arg name "mdz-gui ${STAMP}" \
		--arg body "$body" \
		'{tag_name:$tag,target_commitish:$target,name:$name,body:$body,draft:false,prerelease:true}')
	response=$(curl -fsSL \
		-X POST \
		-H "Authorization: Bearer $token" \
		-H "Accept: application/vnd.github+json" \
		-H "X-GitHub-Api-Version: 2022-11-28" \
		-H "Content-Type: application/json" \
		-d "$payload" \
		"https://api.github.com/repos/${REPO}/releases")
	upload_url=$(printf '%s' "$response" | jq -r '.upload_url' | sed 's/{?name,label}$//')
	[ -n "$upload_url" ] && [ "$upload_url" != "null" ] || fail "GitHub release creation did not return upload_url"
	curl -fsSL \
		-X POST \
		-H "Authorization: Bearer $token" \
		-H "Accept: application/vnd.github+json" \
		-H "X-GitHub-Api-Version: 2022-11-28" \
		-H "Content-Type: application/zip" \
		--data-binary "@build/bin/${ASSET_NAME}" \
		"${upload_url}?name=${ASSET_NAME}" >/dev/null
}

trap cleanup EXIT HUP INT TERM
cd "$ROOT_DIR"

require_command git
require_command go
require_command npm
require_command node
require_command zip
require_command x86_64-w64-mingw32-gcc
require_command x86_64-w64-mingw32-g++

[ -z "$(git status --porcelain)" ] || fail "working tree has uncommitted changes; commit them before creating a release"
COMMIT=$(git rev-parse HEAD)
COMMIT_TIME=$(git show -s --format=%cd --date=format:%Y-%m-%d-%H:%M HEAD)
COMMIT_SHORT=$(printf '%s' "$COMMIT" | cut -c1-7)
APP_VERSION="v0.0.1-${COMMIT_TIME}-${COMMIT_SHORT}"

TMP_DIR=$(mktemp -d)

npm ci --prefix frontend
npm run build --prefix frontend
go test ./... -timeout 120s

if command -v mdbook >/dev/null 2>&1; then
	mdbook build docs/help --dest-dir "$TMP_DIR/mdz-help-preview"
else
	echo "mdbook not found: skipping help preview validation" >&2
fi

if command -v wails >/dev/null 2>&1; then
	WAILS=wails
else
	echo "wails not found: installing v2.15.0 into GOPATH/bin" >&2
	go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0
	WAILS="$(go env GOPATH)/bin/wails"
fi

GOOS=windows \
GOARCH=amd64 \
CGO_ENABLED=1 \
CC=x86_64-w64-mingw32-gcc \
CXX=x86_64-w64-mingw32-g++ \
	"$WAILS" build -platform windows/amd64 -skipbindings -s -ldflags "-X main.version=${APP_VERSION}"

cp README.md build/bin/
go run ./cmd/package-help
(
	cd build/bin
	rm -f "$ASSET_NAME"
	zip -9 "$ASSET_NAME" mdz-gui.exe README.md mdz-gui-help.mdz
)

cat > "$TMP_DIR/release-notes.md" <<'NOTES'
Windows向けビルドです。下のAssetsから日時付きZIPをダウンロードし、展開してmdz-gui.exeを起動してください。

同梱物: mdz-gui.exe / README.md / mdz-gui-help.mdz

通常のMarkdown文書、mdBook、Markdownスライドに対応しています。「新規 → スライドを作成」で作成・編集・全画面発表を利用できます。スライド表示の追加インストールは不要です。

Microsoft Edge WebView2 Runtimeが必要です。Neovimは任意です。mdBookの表示には別途mdBookが必要です。
NOTES

if command -v gh >/dev/null 2>&1; then
	gh auth status >/dev/null
	gh release create "$TAG" "build/bin/${ASSET_NAME}" \
		--repo "$REPO" \
		--target "$COMMIT" \
		--title "mdz-gui ${APP_VERSION}" \
		--prerelease \
		--notes-file "$TMP_DIR/release-notes.md"
else
	publish_with_api
fi

echo "released: ${TAG}"
echo "asset: build/bin/${ASSET_NAME}"
