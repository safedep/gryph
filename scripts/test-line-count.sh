#!/bin/bash
# Test line count tracking for gryph pi-agent hooks
# Usage: ./scripts/test-line-count.sh

set -e

GRYPH="./bin/gryph"
DB="$HOME/.local/share/gryph/audit.db"

echo "=== Testing Line Count Tracking ==="
echo ""

# Test 1: New file - should show only lines_added
echo "Test 1: New file creation"
rm -f /tmp/gryph_test_new.txt

echo '{"hook_event_name":"tool_call","session_id":"test-line-count","cwd":"/tmp","tool_name":"write","tool_call_id":"test-1","input":{"path":"/tmp/gryph_test_new.txt","content":"line1\nline2\nline3\n"}}' | $GRYPH _hook pi-agent tool_call -q 2>/dev/null || true
sleep 0.3

EVENT=$(sqlite3 "$DB" "SELECT id FROM audit_events WHERE tool_name='write' AND payload LIKE '%gryph_test_new.txt%' ORDER BY timestamp DESC LIMIT 1;")
if [ -n "$EVENT" ]; then
    RESULT=$(sqlite3 "$DB" "SELECT payload FROM audit_events WHERE id='$EVENT';" | grep -oE '"lines_added":[0-9]+')
    if [[ "$RESULT" == *'"lines_added":3'* ]]; then
        echo "  ✓ PASS: New file shows +3"
    else
        echo "  ✗ FAIL: Expected lines_added: 3, got $RESULT"
    fi
else
    echo "  ✗ FAIL: No event found"
fi

# Test 2: File overwrite - should show both lines_added and lines_removed
echo "Test 2: File overwrite"
echo -e "oldline1\noldline2\noldline3" > /tmp/gryph_test_overwrite.txt

echo '{"hook_event_name":"tool_call","session_id":"test-line-count","cwd":"/tmp","tool_name":"write","tool_call_id":"test-2","input":{"path":"/tmp/gryph_test_overwrite.txt","content":"newline1\nnewline2\nnewline3\nnewline4\n"}}' | $GRYPH _hook pi-agent tool_call -q 2>/dev/null || true
sleep 0.3

EVENT=$(sqlite3 "$DB" "SELECT id FROM audit_events WHERE tool_name='write' AND payload LIKE '%gryph_test_overwrite.txt%' ORDER BY timestamp DESC LIMIT 1;")
if [ -n "$EVENT" ]; then
    RESULT=$(sqlite3 "$DB" "SELECT payload FROM audit_events WHERE id='$EVENT';" | grep -oE '"lines_(added|removed)":[0-9]+' | tr '\n' ' ')
    if [[ "$RESULT" == *'"lines_added":4'* ]] && [[ "$RESULT" == *'"lines_removed":3'* ]]; then
        echo "  ✓ PASS: Overwrite shows +4 -3"
    else
        echo "  ✗ FAIL: Expected lines_added: 4 and lines_removed: 3, got $RESULT"
    fi
else
    echo "  ✗ FAIL: No event found"
fi

# Test 3: Edit with oldText/newText - should show diff
echo "Test 3: Edit with oldText/newText"
rm -f /tmp/gryph_test_edit.txt

echo '{"hook_event_name":"tool_call","session_id":"test-line-count","cwd":"/tmp","tool_name":"edit","tool_call_id":"test-3","input":{"path":"/tmp/gryph_test_edit.txt","oldText":"old content","newText":"new content here"}}' | $GRYPH _hook pi-agent tool_call -q 2>/dev/null || true
sleep 0.3

EVENT=$(sqlite3 "$DB" "SELECT id FROM audit_events WHERE tool_name='edit' AND payload LIKE '%gryph_test_edit.txt%' ORDER BY timestamp DESC LIMIT 1;")
if [ -n "$EVENT" ]; then
    RESULT=$(sqlite3 "$DB" "SELECT payload FROM audit_events WHERE id='$EVENT';" | grep -oE '"lines_(added|removed)":[0-9]+' | tr '\n' ' ')
    if [[ "$RESULT" == *'"lines_added":1'* ]] && [[ "$RESULT" == *'"lines_removed":1'* ]]; then
        echo "  ✓ PASS: Edit shows +1 -1"
    else
        echo "  ✗ FAIL: Expected lines_added: 1 and lines_removed: 1, got $RESULT"
    fi
else
    echo "  ✗ FAIL: No event found"
fi

# Cleanup
rm -f /tmp/gryph_test_new.txt /tmp/gryph_test_overwrite.txt /tmp/gryph_test_edit.txt

echo ""
echo "=== Tests Complete ==="
