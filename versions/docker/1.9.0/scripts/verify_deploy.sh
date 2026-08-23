#!/bin/bash
set -e
BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN_PASS="${ADMIN_PASS:-}"
GYING_USER="${GYING_USER:-}"
GYING_PASS="${GYING_PASS:-}"
if [ -z "$ADMIN_PASS" ] || [ -z "$GYING_USER" ] || [ -z "$GYING_PASS" ]; then
  echo "usage: ADMIN_PASS=... GYING_USER=... GYING_PASS=... bash scripts/verify_deploy.sh" >&2
  exit 1
fi
COOKIE=/tmp/st_verify

echo "== login =="
curl -s -c $COOKIE -H 'Content-Type: application/json' -d "{\"username\":\"admin\",\"password\":\"$ADMIN_PASS\"}" $BASE/api/login
echo
echo "== settings default =="
curl -s -b $COOKIE $BASE/api/admin/settings
echo
echo "== background settings =="
curl -s -b $COOKIE -H 'Content-Type: application/json' -d '{"background_image_url":"https://example.com/bg.jpg"}' -X PUT $BASE/api/admin/settings
echo
curl -s -b $COOKIE $BASE/api/admin/settings
echo
curl -s $BASE/api/public/settings
echo
echo "== tg users replace =="
curl -s -b $COOKIE -H 'Content-Type: application/json' -d '{"users":[{"tg_id":123456789,"username":"test"}]}' -X PUT $BASE/api/admin/tg/users
echo
curl -s -b $COOKIE $BASE/api/admin/tg/users
echo
echo "== add site =="
curl -s -b $COOKIE -H 'Content-Type: application/json' -d "{\"username\":\"$GYING_USER\",\"password\":\"$GYING_PASS\",\"enabled\":true}" $BASE/api/admin/sites
echo
echo "== list sites =="
curl -s -b $COOKIE $BASE/api/admin/sites
echo
echo "== search =="
curl -s -b $COOKIE "$BASE/api/search?q=%E9%92%A2%E9%93%81%E4%BE%A0" > /tmp/verify_search.json
python3 - <<'PY'
import json
d = json.load(open('/tmp/verify_search.json'))
items = d.get('items', [])
print('TOTAL', len(items), 'elapsed_ms', d.get('elapsed_ms'))
for it in items[:5]:
    print(it.get('title'), '|', it.get('size'), '|', it.get('magnet'))
PY
echo "== p115 fake check =="
curl -s -b $COOKIE -H 'Content-Type: application/json' -d '{"cookie":"UID=1; CID=1; SEID=1; KID=1"}' -X POST $BASE/api/admin/p115/check
echo
