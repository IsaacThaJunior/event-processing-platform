#!/bin/bash

echo "🚀 Testing Task System..."

BASE_URL="http://localhost:8080/tasks"

# ----------------------------
# 1. Simple Image Resize
# ----------------------------
echo "📷 Test 1: Resize Image"

curl -X POST $BASE_URL \
  -H "Content-Type: application/json" \
  -d '{
    "type": "resize_image",
    "priority": "high",
    "payload": {
      "image_url": "https://example.com/photo.jpg",
      "width": 800,
      "height": 600
    }
  }'

echo -e "\n\n"

# ----------------------------
# 2. Scrape URL
# ----------------------------
echo "🔍 Test 2: Scrape URL"

curl -X POST $BASE_URL \
  -H "Content-Type: application/json" \
  -d '{
    "type": "scrape_url",
    "priority": "medium",
    "payload": {
      "url": "https://example.com"
    }
  }'

echo -e "\n\n"

# ----------------------------
# 3. Generate Report
# ----------------------------
echo "📊 Test 3: Generate Report"

curl -X POST $BASE_URL \
  -H "Content-Type: application/json" \
  -d '{
    "type": "generate_report",
    "priority": "low",
    "payload": {
      "date": "2026-04-20"
    }
  }'

echo -e "\n\n"

# ----------------------------
# 4. Chained Task (SCRAPE → REPORT)
# ----------------------------
echo "🔗 Test 4: Chained Task"

curl -X POST $BASE_URL \
  -H "Content-Type: application/json" \
  -d '{
    "type": "scrape_url",
    "priority": "high",
    "payload": {
      "url": "https://example.com"
    },
    "next": {
      "type": "generate_report",
      "priority": "medium",
      "payload": {
        "date": "2026-04-20"
      }
    }
  }'

echo -e "\n\n"

# ----------------------------
# 5. Scheduled Task (delay)
# ----------------------------
echo "⏰ Test 5: Scheduled Task (runs in 30s)"

FUTURE_TIME=$(date -u -v+30s +"%Y-%m-%dT%H:%M:%SZ")

curl -X POST $BASE_URL \
  -H "Content-Type: application/json" \
  -d "{
    \"type\": \"generate_report\",
    \"priority\": \"low\",
    \"execute_at\": \"$FUTURE_TIME\",
    \"payload\": {
      \"date\": \"2026-04-20\"
    }
  }"

echo -e "\n\n"

# ----------------------------
# 6. Chain + Scheduling combined
# ----------------------------
echo "⚡ Test 6: Chain + Delay"

FUTURE_TIME2=$(date -u -v+60S +"%Y-%m-%dT%H:%M:%SZ")

curl -X POST $BASE_URL \
  -H "Content-Type: application/json" \
  -d "{
    \"type\": \"scrape_url\",
    \"priority\": \"high\",
    \"payload\": {
      \"url\": \"https://example.com\"
    },
    \"next\": {
      \"type\": \"generate_report\",
      \"priority\": \"medium\",
      \"execute_at\": \"$FUTURE_TIME2\",
      \"payload\": {
        \"date\": \"2026-04-20\"
      }
    }
  }"

echo -e "\n\n"

echo "✅ Done. Check:"
echo "   - worker logs"
echo "   - /metrics"
echo "   - DB events table"