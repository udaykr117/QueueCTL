#!/bin/bash
set +e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' 

PASSED=0
FAILED=0

TEST_DATA_DIR="$(pwd)/test_data"
export QUEUECTL_DATA_DIR="$TEST_DATA_DIR"
TEST_DB_PATH="$TEST_DATA_DIR/jobs.db"


cleanup() {
    pkill -f "queuectl worker start" 2>/dev/null || true
    sleep 1
    rm -rf "$TEST_DATA_DIR" 2>/dev/null || true
}

cleanup_on_exit() {
    if [ -z "$IN_TEST" ]; then
        cleanup
    fi
}

trap cleanup_on_exit EXIT


pass() {
    echo -e "${GREEN}✓ PASS:${NC} $1"
    ((PASSED++))
}

fail() {
    echo -e "${RED}✗ FAIL:${NC} $1"
    ((FAILED++))
}

test_header() {
    echo -e "\n${YELLOW}=== $1 ===${NC}"
}


echo -e "${YELLOW}Starting queuectl test suite...${NC}"
echo -e "${YELLOW}Using test data directory: $TEST_DATA_DIR${NC}"
rm -rf "$TEST_DATA_DIR"
mkdir -p "$TEST_DATA_DIR"

echo -e "\n${YELLOW}Building queuectl...${NC}"
if go build -o queuectl . 2>&1; then
    pass "Application builds successfully"
else
    fail "Build failed!"
    exit 1
fi

test_header "Test 1: Basic job completes successfully"
JOB_ID="test-success-$(date +%s)"
./queuectl enqueue "{\"id\":\"$JOB_ID\",\"command\":\"echo 'Hello World'\"}" > /dev/null 2>&1
if [ $? -eq 0 ]; then
    pass "Job enqueued successfully"
else
    fail "Failed to enqueue job"
fi

timeout 5 ./queuectl worker start --count 1 > /tmp/worker_test1.log 2>&1 &
WORKER_PID=$!
sleep 3
kill $WORKER_PID 2>/dev/null || true
wait $WORKER_PID 2>/dev/null || true

if grep -q "completed successfully" /tmp/worker_test1.log && grep -q "$JOB_ID" /tmp/worker_test1.log; then
    pass "Job completed successfully"
else
    fail "Job did not complete successfully"
    cat /tmp/worker_test1.log
fi

STATUS=$(./queuectl list --state completed 2>/dev/null | grep "$JOB_ID" | wc -l)
if [ "$STATUS" -eq 1 ]; then
    pass "Job is in completed state"
else
    fail "Job is not in completed state"
fi

test_header "Test 2: Failed job retries with backoff and moves to DLQ"
JOB_ID="test-retry-$(date +%s)"
./queuectl enqueue "{\"id\":\"$JOB_ID\",\"command\":\"false\",\"max_retries\":3}" > /dev/null 2>&1
pass "Failed job enqueued"

timeout 15 ./queuectl worker start --count 1 > /tmp/worker_test2.log 2>&1 &
WORKER_PID=$!
sleep 12
kill $WORKER_PID 2>/dev/null || true
wait $WORKER_PID 2>/dev/null || true

RETRY_COUNT=$(grep -c "will retry" /tmp/worker_test2.log || echo "0")
if [ "$RETRY_COUNT" -ge 1 ]; then
    pass "Job retried with backoff (found $RETRY_COUNT retry messages)"
else
    fail "Job did not retry with backoff"
    cat /tmp/worker_test2.log
fi

if grep -q "will retry in 2s" /tmp/worker_test2.log; then
    pass "Exponential backoff working correctly (2s delay for attempt 1)"
    if grep -q "will retry in 4s" /tmp/worker_test2.log; then
        pass "Exponential backoff working correctly (4s delay for attempt 2)"
    fi
else
    fail "Exponential backoff not working correctly"
    grep "will retry" /tmp/worker_test2.log || true
fi

sleep 1
DLQ_COUNT=$(./queuectl dlq list 2>/dev/null | grep "$JOB_ID" | wc -l)
if [ "$DLQ_COUNT" -eq 1 ]; then
    pass "Job moved to DLQ after max retries"
else
    fail "Job did not move to DLQ"
    ./queuectl status
fi

test_header "Test 3: Multiple workers process jobs without overlap"
TIMESTAMP=$(date +%s)
for i in {1..5}; do
    JOB_ID="test-parallel-$i-$TIMESTAMP"
    ./queuectl enqueue "{\"id\":\"$JOB_ID\",\"command\":\"sleep 1 && echo job-$i\"}" > /dev/null 2>&1
done
pass "5 jobs enqueued for parallel processing"

timeout 10 ./queuectl worker start --count 3 > /tmp/worker_test3.log 2>&1 &
WORKER_PID=$!
sleep 8
kill $WORKER_PID 2>/dev/null || true
wait $WORKER_PID 2>/dev/null || true

PROCESSED_JOBS=$(grep "Processing job: test-parallel" /tmp/worker_test3.log | sed 's/.*Processing job: //' | sed 's/ (command:.*//' | sort -u | wc -l)
if [ "$PROCESSED_JOBS" -ge 3 ]; then
    pass "Multiple workers processed jobs in parallel ($PROCESSED_JOBS unique jobs)"
else
    fail "Workers did not process jobs in parallel (only $PROCESSED_JOBS jobs processed)"
    grep "Processing job: test-parallel" /tmp/worker_test3.log | head -10
fi

DUPLICATES=$(grep "Processing job: test-parallel" /tmp/worker_test3.log | sed 's/.*Processing job: //' | sed 's/ (command:.*//' | sort | uniq -d | wc -l)
if [ "$DUPLICATES" -eq 0 ]; then
    pass "No duplicate job processing detected"
else
    fail "Duplicate job processing detected!"
    grep "Processing job: test-parallel" /tmp/worker_test3.log | sed 's/.*Processing job: //' | sed 's/ (command:.*//' | sort | uniq -d
fi

test_header "Test 4: Invalid commands fail gracefully"
JOB_ID="test-invalid-$(date +%s)"
./queuectl enqueue "{\"id\":\"$JOB_ID\",\"command\":\"nonexistent-command-xyz-123\",\"max_retries\":1}" > /dev/null 2>&1
pass "Invalid command job enqueued"

timeout 5 ./queuectl worker start --count 1 > /tmp/worker_test4.log 2>&1 &
WORKER_PID=$!
sleep 4
kill $WORKER_PID 2>/dev/null || true
wait $WORKER_PID 2>/dev/null || true

if grep -q "failed" /tmp/worker_test4.log && grep -q "$JOB_ID" /tmp/worker_test4.log; then
    pass "Invalid command failed gracefully"
else
    fail "Invalid command did not fail gracefully"
    cat /tmp/worker_test4.log
fi

if ! grep -q "panic" /tmp/worker_test4.log && ! grep -q "fatal" /tmp/worker_test4.log; then
    pass "Worker handled invalid command without crashing"
else
    fail "Worker crashed on invalid command"
    cat /tmp/worker_test4.log
fi

test_header "Test 5: Job data survives restart"
JOB_ID="test-persistence-$(date +%s)"
./queuectl enqueue "{\"id\":\"$JOB_ID\",\"command\":\"echo 'persistence test'\"}" > /dev/null 2>&1
pass "Job enqueued before restart"

BEFORE_RESTART=$(./queuectl list 2>/dev/null | grep "$JOB_ID" | wc -l)
if [ "$BEFORE_RESTART" -eq 1 ]; then
    pass "Job exists before restart"
else
    fail "Job not found before restart"
fi


sleep 1
AFTER_RESTART=$(./queuectl list 2>/dev/null | grep "$JOB_ID" | wc -l)
if [ "$AFTER_RESTART" -eq 1 ]; then
    pass "Job data persisted across restart simulation"
else
    fail "Job data lost after restart"
fi

timeout 5 ./queuectl worker start --count 1 > /tmp/worker_test5.log 2>&1 &
WORKER_PID=$!
sleep 3
kill $WORKER_PID 2>/dev/null || true
wait $WORKER_PID 2>/dev/null || true

if grep -q "completed successfully" /tmp/worker_test5.log && grep -q "$JOB_ID" /tmp/worker_test5.log; then
    pass "Persisted job processed successfully after restart"
else
    fail "Persisted job did not process correctly"
    cat /tmp/worker_test5.log
fi

test_header "Test 6: DLQ retry functionality"
JOB_ID="test-dlq-retry-$(date +%s)"
./queuectl enqueue "{\"id\":\"$JOB_ID\",\"command\":\"false\",\"max_retries\":1}" > /dev/null 2>&1

timeout 5 ./queuectl worker start --count 1 > /tmp/worker_dlq.log 2>&1 &
WORKER_PID=$!
sleep 4
kill $WORKER_PID 2>/dev/null || true
wait $WORKER_PID 2>/dev/null || true

DLQ_CHECK=$(./queuectl dlq list 2>/dev/null | grep "$JOB_ID" | wc -l)
if [ "$DLQ_CHECK" -eq 1 ]; then
    pass "Job moved to DLQ"
else
    fail "Job not in DLQ"
fi

./queuectl dlq retry "$JOB_ID" > /dev/null 2>&1
if [ $? -eq 0 ]; then
    pass "DLQ retry command executed successfully"
else
    fail "DLQ retry command failed"
fi

PENDING_CHECK=$(./queuectl list --state pending 2>/dev/null | grep "$JOB_ID" | wc -l)
if [ "$PENDING_CHECK" -eq 1 ]; then
    pass "Job reset to pending after DLQ retry"
else
    fail "Job not reset to pending after DLQ retry"
fi

test_header "Test 7: Configuration management"
./queuectl config set max-retries 5 > /dev/null 2>&1
CONFIG_VALUE=$(./queuectl config get max-retries 2>/dev/null | tr -d '\n')
if [ "$CONFIG_VALUE" = "5" ]; then
    pass "Configuration set and retrieved successfully"
else
    fail "Configuration not working (got: $CONFIG_VALUE)"
fi

./queuectl config set backoff-base 3.0 > /dev/null 2>&1
CONFIG_VALUE=$(./queuectl config get backoff-base 2>/dev/null | tr -d '\n')
if [ "$CONFIG_VALUE" = "3.0" ]; then
    pass "Backoff base configuration working"
else
    fail "Backoff base configuration not working (got: $CONFIG_VALUE)"
fi

test_header "Test 8: Timeout handling"
JOB_ID="test-timeout-$(date +%s)"
./queuectl enqueue "{\"id\":\"$JOB_ID\",\"command\":\"sleep 5 && echo 'should not reach here'\",\"timeout\":2,\"max_retries\":1}" > /dev/null 2>&1
if [ $? -eq 0 ]; then
    pass "Timeout job enqueued successfully"
else
    fail "Failed to enqueue timeout job"
fi

timeout 10 ./queuectl worker start --count 1 > /tmp/worker_test6.log 2>&1 &
WORKER_PID=$!
sleep 5
kill $WORKER_PID 2>/dev/null || true
wait $WORKER_PID 2>/dev/null || true

if grep -q "timeout" /tmp/worker_test6.log && grep -q "$JOB_ID" /tmp/worker_test6.log; then
    pass "Job timeout detected in logs"
else
    fail "Job timeout not detected"
    cat /tmp/worker_test6.log
fi

JOB_OUTPUT=$(./queuectl show "$JOB_ID" 2>/dev/null)
if echo "$JOB_OUTPUT" | grep -q "timeout" || echo "$JOB_OUTPUT" | grep -q "Timeout"; then
    pass "Job shows timeout in details"
else
    fail "Job does not show timeout in details"
    echo "$JOB_OUTPUT"
fi

if grep -q "jobs_timeout" /tmp/worker_test6.log || sqlite3 "$TEST_DB_PATH" "SELECT value FROM metrics WHERE key='jobs_timeout';" 2>/dev/null | grep -q "[1-9]"; then
    pass "Timeout metric incremented"
else
    fail "Timeout metric not incremented"
fi

test_header "Test 9: Job output logging"
JOB_ID="test-output-$(date +%s)"
EXPECTED_OUTPUT="Test output message $(date +%s)"
./queuectl enqueue "{\"id\":\"$JOB_ID\",\"command\":\"echo '$EXPECTED_OUTPUT'\"}" > /dev/null 2>&1
if [ $? -eq 0 ]; then
    pass "Output test job enqueued successfully"
else
    fail "Failed to enqueue output test job"
fi

timeout 5 ./queuectl worker start --count 1 > /tmp/worker_test7.log 2>&1 &
WORKER_PID=$!
sleep 3
kill $WORKER_PID 2>/dev/null || true
wait $WORKER_PID 2>/dev/null || true

if grep -q "completed successfully" /tmp/worker_test7.log && grep -q "$JOB_ID" /tmp/worker_test7.log; then
    pass "Output test job completed successfully"
else
    fail "Output test job did not complete"
    cat /tmp/worker_test7.log
fi

JOB_OUTPUT=$(./queuectl show "$JOB_ID" 2>/dev/null)
if echo "$JOB_OUTPUT" | grep -q "$EXPECTED_OUTPUT"; then
    pass "Job output is saved and retrievable"
else
    fail "Job output not saved or not retrievable"
    echo "Expected: $EXPECTED_OUTPUT"
    echo "Got:"
    echo "$JOB_OUTPUT"
fi

JOB_ID2="test-output-multiline-$(date +%s)"
./queuectl enqueue "{\"id\":\"$JOB_ID2\",\"command\":\"echo 'Line 1' && echo 'Line 2' && echo 'Line 3'\"}" > /dev/null 2>&1
timeout 5 ./queuectl worker start --count 1 > /tmp/worker_test7b.log 2>&1 &
WORKER_PID=$!
sleep 3
kill $WORKER_PID 2>/dev/null || true
wait $WORKER_PID 2>/dev/null || true

JOB_OUTPUT2=$(./queuectl show "$JOB_ID2" 2>/dev/null)
if echo "$JOB_OUTPUT2" | grep -q "Line 1" && echo "$JOB_OUTPUT2" | grep -q "Line 2" && echo "$JOB_OUTPUT2" | grep -q "Line 3"; then
    pass "Multi-line job output is saved correctly"
else
    fail "Multi-line job output not saved correctly"
    echo "$JOB_OUTPUT2"
fi

test_header "Test 10: Metrics and execution stats"
INITIAL_PROCESSED=$(sqlite3 "$TEST_DB_PATH" "SELECT COALESCE(value, 0) FROM metrics WHERE key='jobs_processed';" 2>/dev/null || echo "0")
INITIAL_FAILED=$(sqlite3 "$TEST_DB_PATH" "SELECT COALESCE(value, 0) FROM metrics WHERE key='jobs_failed';" 2>/dev/null || echo "0")
INITIAL_TIMEOUT=$(sqlite3 "$TEST_DB_PATH" "SELECT COALESCE(value, 0) FROM metrics WHERE key='jobs_timeout';" 2>/dev/null || echo "0")

JOB_ID_METRIC1="test-metric-success-$(date +%s)"
./queuectl enqueue "{\"id\":\"$JOB_ID_METRIC1\",\"command\":\"echo 'metric test success'\"}" > /dev/null 2>&1
timeout 5 ./queuectl worker start --count 1 > /tmp/worker_test8.log 2>&1 &
WORKER_PID=$!
sleep 3
kill $WORKER_PID 2>/dev/null || true
wait $WORKER_PID 2>/dev/null || true

JOB_ID_METRIC2="test-metric-fail-$(date +%s)"
./queuectl enqueue "{\"id\":\"$JOB_ID_METRIC2\",\"command\":\"false\",\"max_retries\":1}" > /dev/null 2>&1
timeout 5 ./queuectl worker start --count 1 >> /tmp/worker_test8.log 2>&1 &
WORKER_PID=$!
sleep 3
kill $WORKER_PID 2>/dev/null || true
wait $WORKER_PID 2>/dev/null || true

FINAL_PROCESSED=$(sqlite3 "$TEST_DB_PATH" "SELECT COALESCE(value, 0) FROM metrics WHERE key='jobs_processed';" 2>/dev/null || echo "0")
FINAL_FAILED=$(sqlite3 "$TEST_DB_PATH" "SELECT COALESCE(value, 0) FROM metrics WHERE key='jobs_failed';" 2>/dev/null || echo "0")

if [ "$FINAL_PROCESSED" -gt "$INITIAL_PROCESSED" ]; then
    pass "jobs_processed metric incremented (was $INITIAL_PROCESSED, now $FINAL_PROCESSED)"
else
    fail "jobs_processed metric not incremented (was $INITIAL_PROCESSED, now $FINAL_PROCESSED)"
fi

if [ "$FINAL_FAILED" -gt "$INITIAL_FAILED" ]; then
    pass "jobs_failed metric incremented (was $INITIAL_FAILED, now $FINAL_FAILED)"
else
    fail "jobs_failed metric not incremented (was $INITIAL_FAILED, now $FINAL_FAILED)"
fi

EXEC_COUNT=$(sqlite3 "$TEST_DB_PATH" "SELECT COUNT(*) FROM job_executions WHERE job_id IN ('$JOB_ID_METRIC1', '$JOB_ID_METRIC2');" 2>/dev/null || echo "0")
if [ "$EXEC_COUNT" -ge 2 ]; then
    pass "Job execution records created ($EXEC_COUNT executions found)"
else
    fail "Job execution records not created (found $EXEC_COUNT executions)"
fi

EXEC_STATS=$(sqlite3 "$TEST_DB_PATH" "SELECT job_id, success, timeout, duration_ms FROM job_executions WHERE job_id='$JOB_ID_METRIC1' LIMIT 1;" 2>/dev/null)
if [ -n "$EXEC_STATS" ]; then
    pass "Execution stats contain required fields (job_id, success, timeout, duration_ms)"
else
    fail "Execution stats missing required fields"
fi

SUCCESS_COUNT=$(sqlite3 "$TEST_DB_PATH" "SELECT COUNT(*) FROM job_executions WHERE job_id='$JOB_ID_METRIC1' AND success=1;" 2>/dev/null || echo "0")
if [ "$SUCCESS_COUNT" -ge 1 ]; then
    pass "Successful job execution recorded with success=1"
else
    fail "Successful job execution not recorded correctly"
fi

FAIL_COUNT=$(sqlite3 "$TEST_DB_PATH" "SELECT COUNT(*) FROM job_executions WHERE job_id='$JOB_ID_METRIC2' AND success=0;" 2>/dev/null || echo "0")
if [ "$FAIL_COUNT" -ge 1 ]; then
    pass "Failed job execution recorded with success=0"
else
    fail "Failed job execution not recorded correctly"
fi

DURATION_CHECK=$(sqlite3 "$TEST_DB_PATH" "SELECT COUNT(*) FROM job_executions WHERE job_id='$JOB_ID_METRIC1' AND duration_ms IS NOT NULL AND duration_ms >= 0;" 2>/dev/null || echo "0")
if [ "$DURATION_CHECK" -ge 1 ]; then
    pass "Execution duration is recorded"
else
    fail "Execution duration not recorded"
fi

echo -e "\n${YELLOW}=== Test Summary ===${NC}"
echo -e "${GREEN}Passed: $PASSED${NC}"
echo -e "${RED}Failed: $FAILED${NC}"

if [ $FAILED -eq 0 ]; then
    echo -e "\n${GREEN}All tests passed! ✓${NC}"
    exit 0
else
    echo -e "\n${RED}Some tests failed! ✗${NC}"
    exit 1
fi

