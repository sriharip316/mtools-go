#!/usr/bin/env bash
set -uo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="${ROOT_DIR}/.local/bin"
MONGODB_BIN="${ROOT_DIR}/.local/mongodb/bin"
MTOOLS="${BIN_DIR}/mtools"
TEST_DIR="${ROOT_DIR}/.local/test_e2e_run"

echo "========================================================"
echo "  mtools — Comprehensive End-to-End (E2E) Test Suite"
echo "           (launch, logfilter, loginfo, & load)"
echo "========================================================"

# 1. Build mtools binary
echo "[1/8] Building mtools binary..."
cd "${ROOT_DIR}"
go build -o "${MTOOLS}" main.go
echo "  -> Built ${MTOOLS} successfully"

PASS=0
FAIL=0

assert_test() {
    local name="$1"
    local cmd="$2"
    local expected_min="$3"
    local expected_max="$4"
    local match_pattern="${5:-}"

    local output
    output=$(eval "${cmd}" 2>&1 || true)
    local line_count
    line_count=$(echo "${output}" | grep -v '^$' | wc -l || true)

    if [ "${line_count}" -ge "${expected_min}" ] && [ "${line_count}" -le "${expected_max}" ]; then
        if [ -n "${match_pattern}" ]; then
            if echo "${output}" | grep -q -- "${match_pattern}"; then
                echo "  [PASS] ${name} (${line_count} lines, matched '${match_pattern}')"
                PASS=$((PASS + 1))
            else
                echo "  [FAIL] ${name} (output did not match '${match_pattern}')"
                echo "         Output snippet: $(echo "${output}" | head -n 2)"
                FAIL=$((FAIL + 1))
            fi
        else
            echo "  [PASS] ${name} (${line_count} lines)"
            PASS=$((PASS + 1))
        fi
    else
        echo "  [FAIL] ${name} (expected ${expected_min}-${expected_max} lines, got ${line_count})"
        echo "         Output snippet: $(echo "${output}" | head -n 2)"
        FAIL=$((FAIL + 1))
    fi
}

cleanup_all() {
    echo "  -> Cleaning up test environments..."
    "${MTOOLS}" launch kill --dir "${TEST_DIR}/single" --signal 9 >/dev/null 2>&1 || true
    "${MTOOLS}" launch kill --dir "${TEST_DIR}/rs" --signal 9 >/dev/null 2>&1 || true
    "${MTOOLS}" launch kill --dir "${TEST_DIR}/sharded" --signal 9 >/dev/null 2>&1 || true
    "${MTOOLS}" launch kill --dir "${TEST_DIR}/auth" --signal 9 >/dev/null 2>&1 || true
    rm -rf "${TEST_DIR}"
}
trap cleanup_all EXIT

rm -rf "${TEST_DIR}"
mkdir -p "${TEST_DIR}"

# 2. launch Standalone (Single) Lifecycle
echo "[2/8] Testing launch: Standalone deployment lifecycle..."
assert_test "launch init --single" "${MTOOLS} launch init --single --port 27400 --dir ${TEST_DIR}/single --binarypath ${MONGODB_BIN}" 0 10
assert_test "launch list (running)" "${MTOOLS} launch list --dir ${TEST_DIR}/single" 2 5 "running"
assert_test "launch list --json" "${MTOOLS} launch list --dir ${TEST_DIR}/single --json" 5 30 '"status": "running"'
assert_test "launch stop" "${MTOOLS} launch stop --dir ${TEST_DIR}/single" 0 10
assert_test "launch list (down)" "${MTOOLS} launch list --dir ${TEST_DIR}/single" 2 5 "down"
assert_test "launch start" "${MTOOLS} launch start --dir ${TEST_DIR}/single --binarypath ${MONGODB_BIN}" 0 10
assert_test "launch list (restarted)" "${MTOOLS} launch list --dir ${TEST_DIR}/single" 2 5 "running"
assert_test "launch restart" "${MTOOLS} launch restart --dir ${TEST_DIR}/single --binarypath ${MONGODB_BIN}" 0 10
assert_test "launch kill" "${MTOOLS} launch kill --dir ${TEST_DIR}/single --signal 9" 0 10

# 3. launch Replica Set Lifecycle
echo "[3/8] Testing launch: Replica Set with Arbiter..."
assert_test "launch init --replicaset" "${MTOOLS} launch init --replicaset --nodes 3 --arbiter --name testRS --port 27410 --dir ${TEST_DIR}/rs --binarypath ${MONGODB_BIN}" 0 20
assert_test "launch list --tags (primary/arbiter)" "${MTOOLS} launch list --dir ${TEST_DIR}/rs --tags" 4 6 "primary"

# Verify write and read on replica set
mongosh --port 27410 --quiet --eval '
let attempts = 0;
while (attempts < 30) {
  try {
    const res = db.hello();
    if (res.isWritablePrimary) break;
  } catch (e) {}
  sleep(200);
  attempts++;
}
const testDb = db.getSiblingDB("rsTest");
testDb.items.insertOne({ msg: "hello replica set", timestamp: new Date() });
' >/dev/null 2>&1
assert_test "Replica set write verification" "mongosh --port 27410 --quiet --eval 'db.getSiblingDB(\"rsTest\").items.countDocuments()'" 1 2 "1"
assert_test "launch kill replica set" "${MTOOLS} launch kill --dir ${TEST_DIR}/rs --signal 9" 0 10

# 4. launch Sharded Cluster Lifecycle
echo "[4/8] Testing launch: Sharded Cluster deployment..."
assert_test "launch init --sharded" "${MTOOLS} launch init --sharded 2 --nodes 1 --config 1 --mongos 1 --port 27430 --dir ${TEST_DIR}/sharded --binarypath ${MONGODB_BIN}" 0 30
assert_test "launch list sharded (mongos & shards)" "${MTOOLS} launch list --dir ${TEST_DIR}/sharded --tags" 4 6 "mongos"

# Verify sharded cluster shard status via mongos
assert_test "Sharded cluster listShards verification" "mongosh --port 27430 --quiet --eval 'db.adminCommand({ listShards: 1 }).shards.length'" 1 2 "2"
assert_test "launch kill sharded cluster" "${MTOOLS} launch kill --dir ${TEST_DIR}/sharded --signal 9" 0 10

# 5. launch Authentication
echo "[5/8] Testing launch: Authentication & Keyfile..."
assert_test "launch init --single --auth" "${MTOOLS} launch init --single --auth --username adminUser --password secretPass --port 27450 --dir ${TEST_DIR}/auth --binarypath ${MONGODB_BIN}" 0 10
assert_test "Auth verification with valid credentials" "mongosh --port 27450 -u adminUser -p secretPass --authenticationDatabase admin --quiet --eval 'db.runCommand({ ping: 1 }).ok'" 1 2 "1"
assert_test "launch kill auth instance" "${MTOOLS} launch kill --dir ${TEST_DIR}/auth --signal 9" 0 10

# 6. logfilter Workload & Filter Assertions
echo "[6/8] Testing logfilter on live MongoDB workload..."
mkdir -p "${TEST_DIR}/filter_env"
"${MONGODB_BIN}/mongod" --dbpath "${TEST_DIR}/filter_env" --logpath "${TEST_DIR}/filter_env/mongod.log" --port 27499 --fork --slowms 0 >/dev/null

LOGFILE="${TEST_DIR}/filter_env/mongod.log"

mongosh --port 27499 --quiet --eval '
const db = db.getSiblingDB("e2etest");
const orders = [];
for (let i = 0; i < 20000; i++) {
  orders.push({ orderId: i, status: i % 2 === 0 ? "active" : "shipped", amount: (i * 17) % 5000, tag: "tag_" + (i % 10), notes: "note " + i });
}
db.orders.insertMany(orders);
db.orders.find({ amount: 99999 }).toArray();
db.orders.createIndex({ status: 1 });
db.orders.find({ status: "active", amount: { $gt: 4000 } }).toArray();
db.orders.distinct("tag");
const adminDb = db.getSiblingDB("admin");
adminDb.setLogLevel(2, "query");
db.users.insertOne({ username: "john_doe", role: "analyst" });
db.users.find({ username: "john_doe" }).toArray();
' >/dev/null 2>&1

assert_test "logfilter basic log parsing" "${MTOOLS} logfilter ${LOGFILE}" 50 1000
assert_test "logfilter --scan" "${MTOOLS} logfilter ${LOGFILE} --scan" 1 10 "COLLSCAN"
assert_test "logfilter --slow 5" "${MTOOLS} logfilter ${LOGFILE} --slow 5" 1 100 "durationMillis"
assert_test "logfilter --fast 50" "${MTOOLS} logfilter ${LOGFILE} --fast 50" 1 1000 "COMMAND"
assert_test "logfilter --namespace" "${MTOOLS} logfilter ${LOGFILE} --namespace e2etest.orders" 1 50 "e2etest.orders"
assert_test "logfilter --command find" "${MTOOLS} logfilter ${LOGFILE} --command find" 1 50 "find"
assert_test "logfilter --pattern" "${MTOOLS} logfilter ${LOGFILE} --pattern '{\"amount\": 1}'" 1 10 "orders"
assert_test "logfilter --component COMMAND" "${MTOOLS} logfilter ${LOGFILE} --component COMMAND" 1 500 "COMMAND"
assert_test "logfilter --level D2" "${MTOOLS} logfilter ${LOGFILE} --level D2" 2 50 "D2"
assert_test "logfilter --planSummary COLLSCAN" "${MTOOLS} logfilter ${LOGFILE} --planSummary COLLSCAN" 1 20 "COLLSCAN"
assert_test "logfilter --word distinct" "${MTOOLS} logfilter ${LOGFILE} --word distinct" 1 10 "distinct"
assert_test "logfilter --exclude --level D2" "${MTOOLS} logfilter ${LOGFILE} --exclude --level D2" 50 1000
assert_test "logfilter --pretty" "${MTOOLS} logfilter ${LOGFILE} --scan --pretty" 10 300
assert_test "logfilter --shorten 80" "${MTOOLS} logfilter ${LOGFILE} --scan --shorten 80" 1 10
assert_test "logfilter --human" "${MTOOLS} logfilter ${LOGFILE} --scan --human" 1 10 "20,000"
assert_test "logfilter stdin streaming" "cat ${LOGFILE} | ${MTOOLS} logfilter --scan" 1 10 "COLLSCAN"

# Mask filter test
"${MTOOLS}" logfilter "${LOGFILE}" --pattern '{"amount": 1}' > "${TEST_DIR}/mask_event.log"
assert_test "logfilter --mask" "${MTOOLS} logfilter ${LOGFILE} --mask ${TEST_DIR}/mask_event.log --mask-size 4" 1 200

# Multi-file merge test
"${MTOOLS}" logfilter "${LOGFILE}" --level I > "${TEST_DIR}/f1.log"
"${MTOOLS}" logfilter "${LOGFILE}" --level D2 > "${TEST_DIR}/f2.log"
assert_test "logfilter multi-file merge" "${MTOOLS} logfilter ${TEST_DIR}/f1.log ${TEST_DIR}/f2.log --markers enum" 5 1000 "{1}"

# 7. loginfo Log Inspection & Analytics Assertions
echo "[7/8] Testing loginfo on live MongoDB workload..."
assert_test "loginfo basic inspection" "${MTOOLS} loginfo ${LOGFILE}" 8 15 "source:"
assert_test "loginfo host/version info" "${MTOOLS} loginfo ${LOGFILE}" 8 15 "version:"
assert_test "loginfo --queries" "${MTOOLS} loginfo ${LOGFILE} --queries --sort sum --rounding 2" 5 50 "QUERIES"
assert_test "loginfo --queries namespace match" "${MTOOLS} loginfo ${LOGFILE} --queries --sort sum" 5 50 "e2etest.orders"
assert_test "loginfo --distinct" "${MTOOLS} loginfo ${LOGFILE} --distinct --distinctmin 1" 2 50 "DISTINCT"
assert_test "loginfo --connections" "${MTOOLS} loginfo ${LOGFILE} --connections --connstats" 5 30 "CONNECTIONS"
assert_test "loginfo --restarts" "${MTOOLS} loginfo ${LOGFILE} --restarts" 5 30 "RESTARTS"
assert_test "loginfo multi-file summary" "${MTOOLS} loginfo ${TEST_DIR}/f1.log ${TEST_DIR}/f2.log" 15 40 "------------------------------------------"

# 8. load Test Data & Workload Simulation Assertions
echo "[8/8] Testing load simulation (initial load + CRUD)..."
assert_test "load --help" "${MTOOLS} load --help" 10 30 "load generates and inserts"
assert_test "load --dry-run" "${MTOOLS} load --dry-run" 20 200 "Sample Phase 1"

# Create custom external schema
cat << 'EOF' > "${TEST_DIR}/custom_schema.json"
{
  "database": "customdb",
  "collections": [
    {
      "name": "metrics",
      "weight": 1.0,
      "crud": {"create": 30, "read": 50, "update": 20, "delete": 0},
      "schema": {
        "type": "object",
        "properties": {
          "_id": {"type": "objectId"},
          "metric": {"type": "string", "enum": ["cpu", "memory", "disk"]},
          "value": {"type": "number", "minimum": 0, "maximum": 100}
        }
      }
    }
  ]
}
EOF
assert_test "load external custom schema --dry-run" "${MTOOLS} load --schema ${TEST_DIR}/custom_schema.json --dry-run" 10 100 "metrics"

# Run live simulation against live mongod on port 27499
assert_test "load live 2-phase simulation" "${MTOOLS} load --uri mongodb://localhost:27499 --database simdb --load-duration 2s --duration 3s --rate 30-80 --interval 1s --drop" 5 50 "simulation summary"
assert_test "simdb users populated" "mongosh --port 27499 --quiet --eval 'db.getSiblingDB(\"simdb\").users.countDocuments() > 0'" 1 2 "true"
assert_test "simdb orders populated" "mongosh --port 27499 --quiet --eval 'db.getSiblingDB(\"simdb\").orders.countDocuments() > 0'" 1 2 "true"

# Shut down filter env mongod
mongosh --port 27499 --eval 'db.getSiblingDB("admin").shutdownServer()' >/dev/null 2>&1 || true

# Summary
echo "========================================================"
echo "  E2E Test Results: ${PASS} Passed, ${FAIL} Failed"
echo "========================================================"

if [ "${FAIL}" -ne 0 ]; then
    exit 1
fi
