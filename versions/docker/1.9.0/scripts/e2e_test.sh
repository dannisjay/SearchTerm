#!/bin/bash
set -e
cd /opt/searchterm

ADMIN_PASS="${ADMIN_PASS:-}"
GYING_USER="${GYING_USER:-}"
GYING_PASS="${GYING_PASS:-}"
if [ -z "$ADMIN_PASS" ] || [ -z "$GYING_USER" ] || [ -z "$GYING_PASS" ]; then
  echo "usage: ADMIN_PASS=... GYING_USER=... GYING_PASS=... bash scripts/e2e_test.sh" >&2
  exit 1
fi

TEST_DATA="$(mktemp -d)"
SECRET_KEY="$(cat /opt/searchterm/data/secret.key 2>/dev/null || openssl rand -hex 32)"
PORT=18080
cat > /tmp/e2e_config.json <<EOF
{"listen":":$PORT","data_dir":"$TEST_DATA","admin_user":"admin","admin_password":"$ADMIN_PASS","secret_key":"$SECRET_KEY"}
EOF

nohup /opt/searchterm/searchterm -config /tmp/e2e_config.json >/tmp/searchterm-e2e.log 2>&1 &
BIN_PID=$!
cleanup() {
  kill "$BIN_PID" 2>/dev/null || true
  rm -rf "$TEST_DATA"
}
trap cleanup EXIT
for i in $(seq 1 20); do
  if curl -s -o /dev/null "http://127.0.0.1:$PORT/api/session"; then
    break
  fi
  sleep 0.5
done
BASE="http://127.0.0.1:$PORT"

echo "== session =="
curl -s "$BASE/api/session"
echo
echo "== login =="
curl -s -c /tmp/st_cookies -H 'Content-Type: application/json' -d "{\"username\":\"admin\",\"password\":\"$ADMIN_PASS\"}" "$BASE/api/login"
echo
echo "== background settings =="
curl -s -b /tmp/st_cookies -H 'Content-Type: application/json' -d '{"background_image_url":"https://example.com/bg.jpg"}' -X PUT "$BASE/api/admin/settings"
echo
curl -s "$BASE/api/public/settings"
echo
echo "== tg users replace =="
curl -s -b /tmp/st_cookies -H 'Content-Type: application/json' -d '{"users":[{"tg_id":1,"username":"test"}]}' -X PUT "$BASE/api/admin/tg/users"
echo
echo "== add site =="
curl -s -b /tmp/st_cookies -H 'Content-Type: application/json' -d "{\"username\":\"$GYING_USER\",\"password\":\"$GYING_PASS\",\"enabled\":true}" "$BASE/api/admin/sites"
echo
echo "== search =="
curl -s -b /tmp/st_cookies "$BASE/api/search?q=%E9%92%A2%E9%93%81%E4%BE%A0" > /tmp/search_result.json
python3 - <<'PY'
import json
d = json.load(open('/tmp/search_result.json'))
items = d.get('items', [])
print('TOTAL', len(items), 'elapsed_ms', d.get('elapsed_ms'))
for it in items[:5]:
    print(it.get('title'), '|', it.get('size'), '|', it.get('magnet'))
PY
