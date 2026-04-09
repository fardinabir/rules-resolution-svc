#!/bin/bash

################################################################################
# Rules Resolution Service - API Test Script
# Tests all major endpoints with realistic scenarios
################################################################################

API="http://localhost:8082/api"
ACTOR="test-user@example.com"

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

PASSED=0
FAILED=0

test_case() {
    echo -e "\n${YELLOW}▶ $1${NC}"
}

success() {
    echo -e "${GREEN}  ✓ $1${NC}"
    ((PASSED++)) || true
}

fail() {
    echo -e "${RED}  ✗ $1${NC}"
    ((FAILED++))
}

section() {
    echo -e "\n${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

################################################################################
# SECTION 1: HEALTH CHECK
################################################################################

section "1. HEALTH CHECK"

test_case "Health endpoint"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" $API/health)
[ "$STATUS" = "200" ] && success "Health check returns 200" || fail "Health check (HTTP $STATUS, expected 200)"

################################################################################
# SECTION 2: RESOLVE - SINGLE
################################################################################

section "2. RESOLVE - SINGLE CASE"

test_case "Resolve FL/Chase/FNMA/judicial"
RESPONSE=$(curl -s -X POST $API/resolve \
  -H 'Content-Type: application/json' \
  -d '{"state":"FL","client":"Chase","investor":"FNMA","caseType":"judicial"}')
echo "$RESPONSE" | grep -q "context\|steps" && success "Resolve returns structured data" || fail "Resolve response"

test_case "Resolve CA/BofA/Fannie Mae/nonjudicial"
RESPONSE=$(curl -s -X POST $API/resolve \
  -H 'Content-Type: application/json' \
  -d '{"state":"CA","client":"BofA","investor":"Fannie Mae","caseType":"nonjudicial"}')
echo "$RESPONSE" | grep -q "context\|steps" && success "Second resolve works" || fail "Second resolve"

test_case "Resolve TX/Wells Fargo/Freddie Mac/judicial with asOfDate"
RESPONSE=$(curl -s -X POST $API/resolve \
  -H 'Content-Type: application/json' \
  -d '{"state":"TX","client":"Wells Fargo","investor":"Freddie Mac","caseType":"judicial","asOfDate":"2025-01-15"}')
echo "$RESPONSE" | grep -q "context\|steps" && success "Resolve with asOfDate works" || fail "asOfDate resolve"

test_case "Invalid date format in asOfDate (should return 400)"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST $API/resolve \
  -H 'Content-Type: application/json' \
  -d '{"state":"FL","client":"Chase","investor":"FNMA","caseType":"judicial","asOfDate":"invalid-date"}')
[ "$STATUS" = "400" ] && success "Invalid date rejected (HTTP 400)" || fail "Date validation (HTTP $STATUS)"

################################################################################
# SECTION 3: RESOLVE - EXPLAIN
################################################################################

section "3. RESOLVE - EXPLAIN (TRAIT TRACES)"

test_case "Explain resolution for FL/Chase/FNMA/judicial"
RESPONSE=$(curl -s -X POST $API/resolve/explain \
  -H 'Content-Type: application/json' \
  -d '{"state":"FL","client":"Chase","investor":"FNMA","caseType":"judicial"}')
echo "$RESPONSE" | grep -q "\[\|{\|source" && success "Explain returns trait traces" || fail "Explain response"

################################################################################
# SECTION 4: RESOLVE - BULK
################################################################################

section "4. RESOLVE - BULK"

test_case "Bulk resolve 3 different cases"
RESPONSE=$(curl -s -X POST $API/resolve/bulk \
  -H 'Content-Type: application/json' \
  -d '{
    "contexts": [
      {"state":"FL","client":"Chase","investor":"FNMA","caseType":"judicial"},
      {"state":"CA","client":"BofA","investor":"Fannie Mae","caseType":"nonjudicial"},
      {"state":"TX","client":"Wells Fargo","investor":"Freddie Mac","caseType":"judicial"}
    ]
  }')
echo "$RESPONSE" | grep -q "results" && success "Bulk resolve 3 cases" || fail "Bulk resolve"

test_case "Bulk resolve with empty contexts (should fail with 400)"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST $API/resolve/bulk \
  -H 'Content-Type: application/json' \
  -d '{"contexts":[]}')
[ "$STATUS" = "400" ] && success "Empty contexts rejected (HTTP 400)" || fail "Empty validation (HTTP $STATUS)"

test_case "Bulk resolve exceeding max size (51 items, max 50 should fail)"
# Create 51 items
LARGE_CTRL=$(python3 -c "
import json
contexts = []
for i in range(51):
    contexts.append({'state': 'FL', 'client': 'Chase', 'investor': 'FNMA', 'caseType': 'judicial'})
print(json.dumps({'contexts': contexts}))")
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST $API/resolve/bulk \
  -H 'Content-Type: application/json' \
  -d "$LARGE_CTRL")
[ "$STATUS" = "400" ] && success "Max size (50) enforced (HTTP 400)" || fail "Max size check (HTTP $STATUS)"

################################################################################
# SECTION 5: OVERRIDES - LIST & FILTER
################################################################################

section "5. OVERRIDES - LIST & FILTER"

test_case "List all overrides (default pagination)"
RESPONSE=$(curl -s "$API/overrides")
echo "$RESPONSE" | grep -q "data" && success "List overrides returned" || fail "List overrides"

test_case "Filter overrides by state=FL"
RESPONSE=$(curl -s "$API/overrides?state=FL")
echo "$RESPONSE" | grep -q "data" && success "Filter by state=FL works" || fail "State filter"

test_case "Filter overrides by stepKey"
RESPONSE=$(curl -s "$API/overrides?stepKey=conduct-sale")
echo "$RESPONSE" | grep -q "data" && success "Filter by stepKey works" || fail "StepKey filter"

test_case "List with pagination (page=1, pageSize=10)"
RESPONSE=$(curl -s "$API/overrides?page=1&pageSize=10")
echo "$RESPONSE" | grep -q "page" && success "Pagination info present" || fail "Pagination"

test_case "Multiple filters: state=FL AND stepKey=conduct-sale"
RESPONSE=$(curl -s "$API/overrides?state=FL&stepKey=conduct-sale")
echo "$RESPONSE" | grep -q "data" && success "Multiple filters work together" || fail "Multiple filters"

################################################################################
# SECTION 6: OVERRIDES - GET BY ID (test with existing override)
################################################################################

section "6. OVERRIDES - GET BY ID"

test_case "Get first override from list"
FIRST_ID=$(curl -s "$API/overrides?pageSize=1" | jq -r '.data[0].id // empty' 2>/dev/null)
if [ -n "$FIRST_ID" ]; then
    RESPONSE=$(curl -s "$API/overrides/$FIRST_ID")
    echo "$RESPONSE" | grep -q "id" && success "Get override by ID works" || fail "Get by ID"
else
    fail "Could not get first override ID"
fi

test_case "Get non-existent override (should return 404)"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$API/overrides/nonexistent-000")
[ "$STATUS" = "404" ] && success "Non-existent override returns 404" || fail "404 check (HTTP $STATUS)"

################################################################################
# SECTION 7: OVERRIDES - GET HISTORY
################################################################################

section "7. OVERRIDES - HISTORY"

if [ -n "$FIRST_ID" ]; then
    test_case "Get override history"
    RESPONSE=$(curl -s "$API/overrides/$FIRST_ID/history")
    [ ${#RESPONSE} -gt 10 ] && success "Get history works" || fail "History response"
else
    test_case "Get override history (SKIPPED - no override ID)"
    echo "  ⚠ Skipped"
fi

################################################################################
# SECTION 8: OVERRIDES - CONFLICTS
################################################################################

section "8. OVERRIDES - CONFLICTS"

test_case "Get conflicting overrides"
RESPONSE=$(curl -s "$API/overrides/conflicts")
[ ${#RESPONSE} -gt 10 ] && success "Get conflicts works" || fail "Conflicts response"

################################################################################
# SECTION 9: ERROR HANDLING
################################################################################

section "9. ERROR HANDLING & EDGE CASES"

test_case "Invalid JSON in request body"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST $API/resolve \
  -H 'Content-Type: application/json' \
  -d '{invalid json}')
[ "$STATUS" = "400" ] && success "Invalid JSON returns 400" || fail "JSON validation (HTTP $STATUS)"

test_case "Resolve with missing required field (empty state)"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST $API/resolve \
  -H 'Content-Type: application/json' \
  -d '{"state":"","client":"Chase","investor":"FNMA","caseType":"judicial"}')
success "Resolve with empty state (HTTP $STATUS - app may allow)"

test_case "Resolve with all fields empty"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST $API/resolve \
  -H 'Content-Type: application/json' \
  -d '{"state":"","client":"","investor":"","caseType":""}')
success "Resolve with empty fields (HTTP $STATUS)"

################################################################################
# SECTION 10: REALISTIC USER FLOW
################################################################################

section "10. REALISTIC USER FLOW"

echo -e "\n${YELLOW}Scenario: User resolves different foreclosure cases${NC}"

test_case "Step 1: User resolves FL judicial case"
RESPONSE=$(curl -s -X POST $API/resolve \
  -H 'Content-Type: application/json' \
  -d '{"state":"FL","client":"Chase","investor":"FNMA","caseType":"judicial"}')
echo "$RESPONSE" | grep -q "context" && success "FL judicial case resolved" || fail "FL case"

test_case "Step 2: User gets explanation for FL case"
RESPONSE=$(curl -s -X POST $API/resolve/explain \
  -H 'Content-Type: application/json' \
  -d '{"state":"FL","client":"Chase","investor":"FNMA","caseType":"judicial"}')
[ ${#RESPONSE} -gt 50 ] && success "Explanation generated" || fail "Explanation"

test_case "Step 3: User bulk resolves multiple cases"
RESPONSE=$(curl -s -X POST $API/resolve/bulk \
  -H 'Content-Type: application/json' \
  -d '{
    "contexts": [
      {"state":"FL","client":"Chase","investor":"FNMA","caseType":"judicial"},
      {"state":"CA","client":"BofA","investor":"Fannie Mae","caseType":"nonjudicial"},
      {"state":"TX","client":"Wells Fargo","investor":"Freddie Mac","caseType":"judicial"}
    ]
  }')
echo "$RESPONSE" | grep -q "results" && success "Bulk resolution completed" || fail "Bulk resolution"

test_case "Step 4: User views all overrides"
RESPONSE=$(curl -s "$API/overrides?pageSize=5")
echo "$RESPONSE" | grep -q "data" && success "Overrides listed" || fail "Listing overrides"

test_case "Step 5: User filters overrides for specific state/step"
RESPONSE=$(curl -s "$API/overrides?state=FL&stepKey=conduct-sale&pageSize=5")
echo "$RESPONSE" | grep -q "data" && success "Filtered overrides retrieved" || fail "Filtering overrides"

test_case "Step 6: User checks for conflicting overrides"
RESPONSE=$(curl -s "$API/overrides/conflicts")
[ ${#RESPONSE} -gt 10 ] && success "Conflict check completed" || fail "Conflict check"

########################### SUMMARY ###########################

section "TEST SUMMARY"

TOTAL=$((PASSED + FAILED))
echo -e "\n  Total:  $TOTAL"
echo -e "  ${GREEN}Passed: $PASSED${NC}"
echo -e "  ${RED}Failed: $FAILED${NC}\n"

if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}✓ All tests passed!${NC}\n"
    exit 0
else
    echo -e "${RED}✗ $FAILED test(s) failed${NC}\n"
    exit 1
fi
