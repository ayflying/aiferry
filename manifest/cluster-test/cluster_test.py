"""Isolated cluster lifecycle. Python standard library only; never reads repository .env."""
import argparse
import base64
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import time
import urllib.error
import urllib.request

HERE = Path(__file__).resolve().parent
PROJECT = "aiferry-cluster-test"
URLS = {"master": "http://127.0.0.1:39517", "worker": "http://127.0.0.1:39518"}
HTTP = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def endpoint(role):
    # 新版Docker的internal网络可能不发布端口；宿主直接访问独立bridge容器IP，
    # 不为测试放开出网，也不挂接现有网络。只读取IP，不读取容器环境变量。
    result = subprocess.run(["docker", "inspect", "--format",
        "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}",
        f"{PROJECT}-{role}-1"], capture_output=True, check=True, text=True)
    address = result.stdout.strip()
    if not address or any(c not in "0123456789." for c in address):
        raise RuntimeError("Invalid isolated container address")
    return f"http://{address}:8080"


class Cluster:
    def __init__(self, state_dir):
        self.state = Path(state_dir).expanduser().resolve()
        # Runtime secrets must remain outside Dockerfile.full's COPY . . context.
        repo = HERE.parents[1]
        if self.state == repo or repo in self.state.parents:
            raise RuntimeError("--state-dir must be outside the checkout (build-context safety)")
        self.envfile = self.state / "compose.env"
        self.credentials = self.state / "credentials.json"

    def init(self, image):
        if not image or any(c.isspace() for c in image) or image.endswith(":latest"):
            raise RuntimeError("Supply a dedicated CI test tag or digest, never latest")
        self.state.mkdir(mode=0o700, parents=True, exist_ok=True)
        if self.envfile.exists() or self.credentials.exists():
            raise RuntimeError("State already exists; reuse it, do not rotate live volume passwords")
        values = {"CLUSTER_TEST_IMAGE": image, "TEST_MYSQL_ROOT_PASSWORD": secrets.token_hex(32),
                  "TEST_MYSQL_PASSWORD": secrets.token_hex(32), "TEST_REDIS_PASSWORD": secrets.token_hex(32),
                  "TEST_MASTER_KEY": base64.b64encode(secrets.token_bytes(32)).decode(),
                  "TEST_CLUSTER_SECRET": secrets.token_hex(32)}
        self._private_write(self.envfile, "".join(f"{k}={v}\n" for k, v in values.items()))
        self._private_write(self.credentials, json.dumps({"api_key": "sk-cluster-test-" + secrets.token_hex(24),
                                                       "upstream_key": secrets.token_hex(32)}))
        print("Initialized fresh test-only credentials outside checkout (values not printed).")

    @staticmethod
    def _private_write(path, text):
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8", newline="\n") as stream:
            stream.write(text)

    def compose(self, *args, stdin=None):
        if not self.envfile.is_file():
            raise RuntimeError("Run init first")
        env = os.environ.copy()
        # Ignore shell overrides and prevent Compose from loading any implicit .env.
        for key in list(env):
            if key.startswith(("TEST_", "COMPOSE_")) or key == "CLUSTER_TEST_IMAGE":
                del env[key]
        env["COMPOSE_DISABLE_ENV_FILE"] = "1"
        cmd = ["docker", "compose", "--project-name", PROJECT, "--project-directory", str(HERE),
               "--env-file", str(self.envfile), "--file", str(HERE / "compose.yaml"), *args]
        result = subprocess.run(cmd, input=stdin, capture_output=True, env=env)
        if result.returncode:
            # Do not echo raw command output: compose config/errors may contain credentials.
            raise RuntimeError(f"Isolated compose command {args[0]} failed (exit {result.returncode}); inspect locally without sharing secrets")
        return result.stdout

    def sql(self, query):
        return self.compose("exec", "-T", "mysql", "sh", "-ec",
            'MYSQL_PWD="$MYSQL_PASSWORD" exec mysql -u"$MYSQL_USER" "$MYSQL_DATABASE" --batch --skip-column-names',
            stdin=query.encode()).decode().strip()

    def seed(self):
        wait_http("master")
        sql = self.compose("exec", "-T", "master", "/app/cluster-test-fixture", stdin=self.credentials.read_bytes())
        self.sql(sql.decode())
        # SQL bypasses cache invalidation hooks; restart master before worker bootstraps.
        self.compose("restart", "master")
        wait_http("master")
        print("Seeded user/channel/model/API key; existing balances and usage preserved.")

    def up(self):
        self.compose("up", "-d", "--wait", "mysql", "redis", "mock", "master")
        self.seed()
        self.compose("up", "-d", "--wait", "worker")
        wait_http("worker")
        print("Isolated cluster ready; verifier uses private bridge IPs (loopback publishing is Docker-version dependent).")

    def request(self, role, stream=False):
        key = json.loads(self.credentials.read_text(encoding="utf-8"))["api_key"]
        body = {"model": "cluster-test-model", "messages": [{"role": "user", "content": "cluster fixture"}],
                "stream": stream}
        if stream:
            body["stream_options"] = {"include_usage": True}
        req = urllib.request.Request(endpoint(role) + "/v1/chat/completions", json.dumps(body).encode(),
            {"Authorization": "Bearer " + key, "Content-Type": "application/json"})
        try:
            response = HTTP.open(req, timeout=30)
        except urllib.error.HTTPError as error:
            # 仅独立fixture请求，无真实用户输入或上游密钥。
            detail = error.read(2048).decode(errors="replace")
            raise RuntimeError(f"{role} fixture HTTP {error.code}: {detail}") from None
        with response:
            text = response.read().decode()
            assert response.status == 200
            if stream:
                assert "text/event-stream" in response.headers.get("Content-Type", "")
                events = [line[6:] for line in text.splitlines() if line.startswith("data: ")]
                assert "[DONE]" in events and any("OK" in event for event in events)
                assert any(json.loads(event).get("usage", {}).get("total_tokens") == 12
                           for event in events if event != "[DONE]")
            else:
                payload = json.loads(text)
                assert payload["choices"][0]["message"]["content"] == "OK"
                assert payload["usage"]["total_tokens"] == 12


def wait_http(role, timeout=120):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            with HTTP.open(endpoint(role) + "/healthz", timeout=2) as response:
                if response.status == 200:
                    return
        except (OSError, urllib.error.URLError):
            pass
        time.sleep(1)
    raise RuntimeError(f"{role} health timed out")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--state-dir", default=str(Path.home() / ".aiferry-cluster-test"))
    sub = parser.add_subparsers(dest="command", required=True)
    init = sub.add_parser("init")
    init.add_argument("--image", required=True)
    for command in ("up", "seed", "check", "recovery", "policy", "down", "destroy", "validate"):
        sub.add_parser(command)
    args = parser.parse_args()
    cluster = Cluster(args.state_dir)
    if args.command == "init":
        cluster.init(args.image)
    elif args.command == "up":
        cluster.up()
    elif args.command == "seed":
        cluster.seed()
    elif args.command in ("check", "recovery", "policy"):
        from verify import check, recovery, policy
        {"check": check, "recovery": recovery, "policy": policy}[args.command](cluster)
    elif args.command == "validate":
        cluster.compose("config", "--quiet")
        print("Compose syntax/interpolation valid (no secrets printed).")
    elif args.command == "down":
        cluster.compose("down")
        print("Stopped test project; volumes and credentials retained.")
    elif args.command == "destroy":
        cluster.compose("down", "--volumes", "--remove-orphans")
        print("Deleted only test project containers/network/volumes; runtime credentials retained outside checkout.")


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        # HTTP exceptions do not print Authorization or body.
        print(f"FAIL: {error}", file=sys.stderr)
        sys.exit(1)
