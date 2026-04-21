#!/bin/bash

echo "🚀 Testing Task System..."

BASE_URL="http://localhost:8080/tasks"

# Linux-safe date (works in Docker)
NOW=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
PLUS_30=$(date -u -v+30S +"%Y-%m-%dT%H:%M:%SZ")
PLUS_60=$(date -u -v+60S +"%Y-%m-%dT%H:%M:%SZ")
PLUS_90=$(date -u -v+90S +"%Y-%m-%dT%H:%M:%SZ")

# ----------------------------
# 1. Simple Resize (HIGH)
# ----------------------------
# echo "📷 Test 1: Resize Image"

# curl -s -X POST $BASE_URL \
#   -H "Content-Type: application/json" \
#   -d '{
#     "type": "resize_image",
#     "priority": "high",
#     "payload": {
#       "image_url": "https://picsum.photos/200",
#       "width": 500,
#       "height": 300
#     }
#   }'

# echo -e "\n\n"

# ----------------------------
# 2. Scrape Different URL (MEDIUM)
# ----------------------------
# echo "🔍 Test 2: Scrape URL"

# curl -s -X POST $BASE_URL \
#   -H "Content-Type: application/json" \
#   -d '{
#     "type": "scrape_url",
#     "priority": "medium",
#     "payload": {
#       "url": "https://httpbin.org/html"
#     }
#   }'

# echo -e "\n\n"

# ----------------------------
# 3. Report Different Date (LOW)
# ----------------------------
# echo "📊 Test 3: Generate Report"

# curl -s -X POST $BASE_URL \
#   -H "Content-Type: application/json" \
#   -d '{
#     "type": "generate_report",
#     "priority": "low",
#     "payload": {
#       "date": "2026-01-01"
#     }
#   }'

# echo -e "\n\n"

# ----------------------------
# 4. Deep Chain (SCRAPE → RESIZE → REPORT)
# ----------------------------
echo "🔗 Test 4: Deep Chain"

curl -s -X POST $BASE_URL \
  -H "Content-Type: application/json" \
  -d '{
    "type": "scrape_url",
    "priority": "high",
    "payload": {
      "url": "https://example.org"
    },
    "next": {
      "type": "resize_image",
      "priority": "medium",
      "payload": {
        "image_url": "https://picsum.photos/300",
        "width": 600,
        "height": 400
      },
      "next": {
        "type": "generate_report",
        "priority": "low",
        "payload": {
          "date": "2026-02-01"
        }
      }
    }
  }'

echo -e "\n\n"

# ----------------------------
# 5. Scheduled Only (30s delay)
# ----------------------------
# echo "⏰ Test 5: Scheduled Task"

# curl -s -X POST $BASE_URL \
#   -H "Content-Type: application/json" \
#   -d "{
#     \"type\": \"generate_report\",
#     \"priority\": \"low\",
#     \"execute_at\": \"$PLUS_30\",
#     \"payload\": {
#       \"date\": \"2026-03-01\"
#     }
#   }"

# echo -e "\n\n"

# ----------------------------
# 6. Chain + Scheduling (NEXT delayed)
# ----------------------------
# echo "⚡ Test 6: Chain + Delay"

# curl -s -X POST $BASE_URL \
#   -H "Content-Type: application/json" \
#   -d "{
#     \"type\": \"scrape_url\",
#     \"priority\": \"high\",
#     \"payload\": {
#       \"url\": \"https://golang.org\"
#     },
#     \"next\": {
#       \"type\": \"generate_report\",
#       \"priority\": \"medium\",
#       \"execute_at\": \"$PLUS_60\",
#       \"payload\": {
#         \"date\": \"2026-04-01\"
#       }
#     }
#   }"

# echo -e "\n\n"

# ----------------------------
# 7. Fully Scheduled Chain (both delayed)
# ----------------------------
# echo "🧠 Test 7: Full Scheduled Chain"

# curl -s -X POST $BASE_URL \
#   -H "Content-Type: application/json" \
#   -d "{
#     \"type\": \"resize_image\",
#     \"priority\": \"medium\",
#     \"execute_at\": \"$PLUS_30\",
#     \"payload\": {
#       \"image_url\": \"https://picsum.photos/400\",
#       \"width\": 700,
#       \"height\": 500
#     },
#     \"next\": {
#       \"type\": \"generate_report\",
#       \"priority\": \"low\",
#       \"execute_at\": \"$PLUS_90\",
#       \"payload\": {
#         \"date\": \"2026-05-01\"
#       }
#     }
#   }"

# echo -e "\n\n"

echo "✅ Done. Check:"
echo "   - worker logs"
echo "   - /metrics"
echo "   - DB (parent_id, chain_id)"