#!/usr/bin/env bash
# Render an HTML file or .eml in classic Outlook via outlook-render and save the PNG.
#
#   OUTLOOK_RENDER_URL=http://my-laptop:8787 OUTLOOK_RENDER_TOKEN=... \
#     ./render.sh email.html out.png "Subject line" [extra query, e.g. "pages=5"]
set -euo pipefail

in="${1:?usage: render.sh <file.html|file.eml> [out.png] [subject] [query]}"
out="${2:-${in%.*}.png}"
subject="${3:-Outlook render test}"
extra="${4:-}"
url="${OUTLOOK_RENDER_URL:?set OUTLOOK_RENDER_URL, e.g. http://my-laptop:8787}"
: "${OUTLOOK_RENDER_TOKEN:?set OUTLOOK_RENDER_TOKEN}"

case "$in" in
  *.eml) type="message/rfc822" ;;
  *)     type="text/html; charset=utf-8" ;;
esac

status=$(curl -sS -o "$out" -w '%{http_code}' -X POST \
  -H "Authorization: Bearer $OUTLOOK_RENDER_TOKEN" \
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
