#!/bin/bash
set -e
ADMIN_PASS="${ADMIN_PASS:-}"
if [ -z "$ADMIN_PASS" ]; then
  echo "usage: ADMIN_PASS=... bash scripts/check_api.sh" >&2
  exit 1
fi
BASE=http://127.0.0.1:8080
curl -s -c /tmp/st2 -H 'Content-Type: application/json' -d "{\"username\":\"admin\",\"password\":\"$ADMIN_PASS\"}" $BASE/api/login
echo
curl -s -b /tmp/st2 $BASE/api/admin/p115
echo
curl -s -b /tmp/st2 -H 'Content-Type: application/json' -d '{"save_paths":[{"id":"123","name":"test"}]}' -X PUT $BASE/api/admin/p115
echo
curl -s -b /tmp/st2 $BASE/api/admin/p115
echo
curl -s -b /tmp/st2 -H 'Content-Type: application/json' -d '{"cookie":"UID=1; CID=1; SEID=1; KID=1"}' -X POST $BASE/api/admin/p115/check
echo
curl -s -b /tmp/st2 -H 'Content-Type: application/json' -d '{"magnet":"magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","save_path_id":"123"}' -X POST $BASE/api/search/save115
echo
curl -s -b /tmp/st2 -o /dev/null -w 'index_http=%{http_code}\n' $BASE/
