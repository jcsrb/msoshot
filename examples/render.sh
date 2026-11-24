#!/usr/bin/env bash
# Render an HTML file or .eml in classic Outlook via msoshot and save the PNG.
#
#   MSOSHOT_URL=http://my-laptop:8787 MSOSHOT_TOKEN=... \
#     ./render.sh email.html out.png "Subject line" [extra query, e.g. "pages=5"]
set -euo pipefail

in="${1:?usage: render.sh <file.html|file.eml> [out.png] [subject] [query]}"
out="${2:-${in%.*}.png}"
subject="${3:-msoshot test}"
extra="${4:-}"
url="${MSOSHOT_URL:?set MSOSHOT_URL, e.g. http://my-laptop:8787}"
: "${MSOSHOT_TOKEN:?set MSOSHOT_TOKEN}"

case "$in" in
  *.eml) type="message/rfc822" ;;
  *)     type="text/html; charset=utf-8" ;;
esac

status=$(curl -sS -o "$out" -w '%{http_code}' -X POST \
  -H "Authorization: Bearer $MSOSHOT_TOKEN" \
  -H "Content-Type: $type" \
  -H "X-Subject: $subject" \
  --data-binary @"$in" \
  "$url/render${extra:+?$extra}")

if [ "$status" != "200" ]; then
  echo "render failed ($status): $(cat "$out")" >&2
  rm -f "$out"
  exit 1
fi
echo "$out"
