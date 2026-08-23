#!/bin/sh
for f in \
  /app/app/services/sources/rss/web.py \
  /app/app/services/sources/rss/site_detail.py \
  /app/app/services/sources/rss/url_builder.py \
  /app/app/services/sources/rss/detail_candidates.py \
  /app/app/services/sources/rss/site_page.py \
  /app/app/services/sources/rss/test.py \
  /app/app/services/link/__init__.py
  do
    if [ -f "$f" ]; then
      echo "===== $f ====="
      cat "$f"
      echo
    fi
  done
