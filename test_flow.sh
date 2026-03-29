#!/bin/bash
# test_flow.sh

# REPLACE THESE WITH YOUR ACTUAL CONTAINER NAMES
REDIS_CONTAINER="event-redis"        # Change this to your Redis container name
POSTGRES_CONTAINER="event-postgres"  # Change this to your Postgres container name
DATABASE_NAME="events"                    # Change this to your database name
DATABASE_USER="postgres"                  # Change this to your database user

echo "🚀 Testing WhatsApp Automation System"
echo "====================================="
echo "Redis container: $REDIS_CONTAINER"
echo "Postgres container: $POSTGRES_CONTAINER"
echo ""

# Test Case 1: Image Resize
echo "📸 Test 1: Image Resize"
EVENT_ID_1="test-resize-$(date +%s)"
WHATSAPP_ID_1="whatsapp_$(date +%s)_resize"

docker exec $POSTGRES_CONTAINER psql -U $DATABASE_USER -d $DATABASE_NAME <<EOF
INSERT INTO events (
    id, type, payload, created_at, whatsapp_message_id, 
    from_number, command, status, updated_at
) VALUES (
    '$EVENT_ID_1',
    'resize_image',
    '{"image_url": "https://example.com/photo.jpg", "width": 800, "height": 600}',
    NOW(),
    '$WHATSAPP_ID_1',
    '+1234567890',
    'resize_image',
    'pending',
    NOW()
);
EOF

if [ $? -eq 0 ]; then
    echo "   ✅ Event created in PostgreSQL"
else
    echo "   ❌ Failed to create event"
    exit 1
fi

docker exec $REDIS_CONTAINER redis-cli LPUSH events_queue "$EVENT_ID_1"
if [ $? -eq 0 ]; then
    echo "   ✅ Task pushed to Redis: $EVENT_ID_1"
else
    echo "   ❌ Failed to push to Redis"
fi

# Test Case 2: URL Scrape
echo ""
echo "🔍 Test 2: URL Scrape"
EVENT_ID_2="test-scrape-$(date +%s)"
WHATSAPP_ID_2="whatsapp_$(date +%s)_scrape"

docker exec $POSTGRES_CONTAINER psql -U $DATABASE_USER -d $DATABASE_NAME <<EOF
INSERT INTO events (
    id, type, payload, created_at, whatsapp_message_id,
    from_number, command, status, updated_at
) VALUES (
    '$EVENT_ID_2',
    'scrape_url',
    '{"url": "https://example.com"}',
    NOW(),
    '$WHATSAPP_ID_2',
    '+9876543210',
    'scrape_url',
    'pending',
    NOW()
);
EOF

if [ $? -eq 0 ]; then
    echo "   ✅ Event created in PostgreSQL"
else
    echo "   ❌ Failed to create event"
fi

docker exec $REDIS_CONTAINER redis-cli LPUSH events_queue "$EVENT_ID_2"
echo "   ✅ Task pushed to Redis: $EVENT_ID_2"

# Test Case 3: Generate Report
echo ""
echo "📊 Test 3: Generate Report"
EVENT_ID_3="test-report-$(date +%s)"
WHATSAPP_ID_3="whatsapp_$(date +%s)_report"

docker exec $POSTGRES_CONTAINER psql -U $DATABASE_USER -d $DATABASE_NAME <<EOF
INSERT INTO events (
    id, type, payload, created_at, whatsapp_message_id,
    from_number, command, status, updated_at
) VALUES (
    '$EVENT_ID_3',
    'generate_report',
    '{"date": "2024-01-15"}',
    NOW(),
    '$WHATSAPP_ID_3',
    '+5555555555',
    'generate_report',
    'pending',
    NOW()
);
EOF

if [ $? -eq 0 ]; then
    echo "   ✅ Event created in PostgreSQL"
else
    echo "   ❌ Failed to create event"
fi

docker exec $REDIS_CONTAINER redis-cli LPUSH events_queue "$EVENT_ID_3"
echo "   ✅ Task pushed to Redis: $EVENT_ID_3"

# Optional: Create idempotency keys
echo ""
echo "🔐 Creating idempotency keys..."

docker exec $POSTGRES_CONTAINER psql -U $DATABASE_USER -d $DATABASE_NAME <<EOF
INSERT INTO idempotency_keys (key, event_id, expires_at, metadata) VALUES 
('$WHATSAPP_ID_1', '$EVENT_ID_1', NOW() + INTERVAL '30 days', '{"from_number":"+1234567890","command":"resize_image"}'),
('$WHATSAPP_ID_2', '$EVENT_ID_2', NOW() + INTERVAL '30 days', '{"from_number":"+9876543210","command":"scrape_url"}'),
('$WHATSAPP_ID_3', '$EVENT_ID_3', NOW() + INTERVAL '30 days', '{"from_number":"+5555555555","command":"generate_report"}')
ON CONFLICT (key) DO NOTHING;
EOF

echo "   ✅ Idempotency keys created"

# Show summary
echo ""
echo "====================================="
echo "✨ Test events pushed successfully!"
echo ""
echo "📊 Monitor Commands:"
echo ""
echo "1. Check Redis queue length:"
echo "   docker exec $REDIS_CONTAINER redis-cli LLEN events_queue"
echo ""
echo "2. Check events in PostgreSQL:"
echo "   docker exec $POSTGRES_CONTAINER psql -U $DATABASE_USER -d $DATABASE_NAME -c \"SELECT id, status, command FROM events WHERE id LIKE 'test-%' ORDER BY created_at DESC LIMIT 5;\""
echo ""
echo "3. Check delivery logs:"
echo "   docker exec $POSTGRES_CONTAINER psql -U $DATABASE_USER -d $DATABASE_NAME -c \"SELECT event_id, status, attempt, created_at FROM event_delivery_logs ORDER BY created_at DESC LIMIT 10;\""
echo ""
echo "4. Watch Redis queue in real-time:"
echo "   watch -n 1 'docker exec $REDIS_CONTAINER redis-cli LLEN events_queue'"
echo ""
echo "5. Watch PostgreSQL events:"
echo "   watch -n 2 'docker exec $POSTGRES_CONTAINER psql -U $DATABASE_USER -d $DATABASE_NAME -c \"SELECT id, status, command FROM events ORDER BY updated_at DESC LIMIT 5;\"'"
echo ""
echo "6. Check Dead Letter Queue (if any failures):"
echo "   docker exec $REDIS_CONTAINER redis-cli LRANGE dead_letter_queue 0 -1"
echo ""
echo "🎯 Your worker should now process these tasks!"