"""Network-local deterministic OpenAI fixture; derived from hack/mock-openai.py."""
import json
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def send_json(self, status, payload):
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/healthz":
            return self.send_json(200, {"ok": True})
        if self.path == "/v1/models":
            return self.send_json(200, {"object": "list", "data": [{"id": "mock-gpt", "object": "model"}]})
        self.send_json(404, {"error": {"message": "Not found"}})

    def do_POST(self):
        if self.path not in ("/v1/chat/completions", "/chat/completions"):
            return self.send_json(404, {"error": {"message": "Not found"}})
        body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", "0"))))
        usage = {"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12,
                 "prompt_tokens_details": {"cached_tokens": 3}}
        if not body.get("stream"):
            return self.send_json(200, {"id": "chatcmpl_cluster_test", "object": "chat.completion",
                "model": "mock-gpt", "choices": [{"index": 0, "message": {"role": "assistant", "content": "OK"},
                "finish_reason": "stop"}], "usage": usage})
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.end_headers()
        for event in [
            {"id": "chatcmpl_cluster_test", "object": "chat.completion.chunk", "choices": [{"index": 0, "delta": {"content": "OK"}}]},
            {"id": "chatcmpl_cluster_test", "object": "chat.completion.chunk", "choices": [], "usage": usage},
        ]:
            self.wfile.write(("data: " + json.dumps(event) + "\n\n").encode())
            self.wfile.flush()
            time.sleep(0.02)
        self.wfile.write(b"data: [DONE]\n\n")
        self.wfile.flush()


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8090), Handler).serve_forever()
