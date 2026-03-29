#!/bin/bash
# test_flow.sh

echo "🚀 Pushing test events via HTTP endpoint..."

# Test 1: Image Resize
curl -X POST http://localhost:8080/test/push \
  -H "Content-Type: application/json" \
  -d '{
    "command": "resize_image",
    "from_number": "+1234567890",
    "payload": "{\"image_url\": \"https://example.com/photo.jpg\", \"width\": 800, \"height\": 600}",
    "original_text": "resize image from https://example.com/photo.jpg to 800x600"
  }'

echo ""
echo ""

# Test 2: URL Scrape
curl -X POST http://localhost:8080/test/push \
  -H "Content-Type: application/json" \
  -d '{
    "command": "scrape_url",
    "from_number": "+9876543210",
    "payload": "{\"url\": \"https://example.com\"}",
    "original_text": "scrape https://example.com"
  }'

echo ""
echo ""

# Test 3: Generate Report
curl -X POST http://localhost:8080/test/push \
  -H "Content-Type: application/json" \
  -d '{
    "command": "generate_report",
    "from_number": "+5555555555",
    "payload": "{\"date\": \"2024-01-15\"}",
    "original_text": "generate report for 2024-01-15"
  }'

echo ""
echo ""
echo "✨ Test events pushed! Check your worker logs."