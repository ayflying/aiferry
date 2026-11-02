"""HTTP and SQL assertions; all mutations are scoped to aiferry-cluster-test."""
from decimal import Decimal
import json
import time

FIXTURE = 990001
COST = Decimal("0.00001250")  # (10-3)*1 + 3*0.5 + 2*2, per million tokens.


def snapshot(cluster):
    values = cluster.sql("""SELECT
 (SELECT COUNT(*) FROM usage_logs WHERE api_key_id=990001),
 (SELECT balance FROM users WHERE id=990001),
 (SELECT spent_amount FROM api_keys WHERE id=990001),
 (SELECT COALESCE(SUM(estimated_cost),0) FROM usage_logs WHERE api_key_id=990001)
""").split("\t")
    return int(values[0]), *(Decimal(value) for value in values[1:])


def settle(cluster, before, count, timeout=60):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        after = snapshot(cluster)
        if after[0] == before[0] + count:
            expected = COST * count
            assert after[1] == before[1] - expected, "User balance debit mismatch"
            assert after[2] == before[2] + expected, "API key spend mismatch"
            assert after[3] == before[3] + expected, "Usage cost mismatch"
            return after
        if after[0] > before[0] + count:
            raise AssertionError("Unexpected/duplicate usage rows")
        time.sleep(1)
    raise AssertionError("Usage flush/debit did not converge within 60 seconds")


def check(cluster):
    from cluster_test import wait_http
    wait_http("master")
    wait_http("worker")
    before = snapshot(cluster)
    for role in ("master", "worker"):
        for stream in (False, True):
            cluster.request(role, stream)
    after = settle(cluster, before, 4)
    result = cluster.sql(f"""SELECT COUNT(*),SUM(node_id=0),SUM(node_id>0),SUM(is_stream=1),
 SUM(input_tokens=10 AND cached_input_tokens=3 AND output_tokens=2 AND total_tokens=12),
 COUNT(DISTINCT request_id) FROM usage_logs WHERE api_key_id={FIXTURE}
 AND id IN (SELECT id FROM (SELECT id FROM usage_logs WHERE api_key_id={FIXTURE} ORDER BY id DESC LIMIT 4) recent)
""")
    assert result.split("\t") == ["4", "2", "2", "2", "4", "4"], "Master/worker token/source attribution mismatch"
    node = cluster.sql("SELECT address FROM cluster_nodes WHERE name='test-worker' AND status=1")
    assert node == "http://worker:8080", "Worker callback URL not registered"
    time.sleep(5)
    assert snapshot(cluster) == after, "Duplicate debit after subsequent flush"
    print("PASS: master/worker ordinary + SSE; exact tokens, source attribution, usage flush, balance/API spend, no duplicate debit.")


def pending(cluster):
    script = "import sqlite3; c=sqlite3.connect('file:/worker/worker.db?mode=ro',uri=True); print(c.execute('SELECT COUNT(*) FROM usage_logs').fetchone()[0])"
    # Read-only mount of this project's worker volume; no host or production mounts.
    return int(cluster.compose("run", "--rm", "--no-deps", "--volume",
        "aiferry-cluster-test_worker-data:/worker:ro", "mock", "python", "-c", script).strip())


def recovery(cluster):
    from cluster_test import wait_http
    check(cluster)
    before = snapshot(cluster)
    cluster.compose("stop", "master")
    stopped_worker = False
    try:
        cluster.request("worker", False)
        cluster.request("worker", True)
        assert pending(cluster) == 2, "Worker did not durably queue offline usage"
        # Bootstrap intentionally requires master. Stop worker, prove disk retention,
        # bring master back, then restart worker from the same persistent volume.
        cluster.compose("stop", "worker")
        stopped_worker = True
        assert pending(cluster) == 2, "Pending rows lost at worker shutdown"
    finally:
        cluster.compose("start", "master")
        wait_http("master")
        if stopped_worker:
            cluster.compose("start", "worker")
        wait_http("worker")
    after = settle(cluster, before, 2)
    deadline = time.monotonic() + 30
    while pending(cluster) and time.monotonic() < deadline:
        time.sleep(1)
    assert pending(cluster) == 0, "Acknowledged outbox rows not removed"
    time.sleep(5)
    assert snapshot(cluster) == after, "Recovery caused duplicate accounting"
    print("PASS: master outage, worker ordinary/SSE continuity, durable pending rows, worker restart, replay flush, exact-once debit and outbox drain.")
