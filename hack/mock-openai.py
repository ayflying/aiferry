import json
import os
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Handler(BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        return

    def send_json(self, status, payload):
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path.startswith("/v1/models"):
            self.send_json(200, {"object": "list", "data": [
                {"id": "zeta-model", "object": "model"},
                {"id": "mock-gpt", "object": "model"},
                {"id": "alpha-model", "object": "model"},
            ]})
            return
        if self.path.startswith("/v1/usage"):
            self.send_json(200, {"remaining": 42.5, "used": 7.5, "unit": "USD"})
            return
        self.send_json(404, {"error": {"type": "not_found", "message": "Not found"}})

    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        payload = json.loads(self.rfile.read(length) or b"{}")
        if os.environ.get("FAIL_REQUESTS") == "1":
            self.send_json(503, {"error": {"type": "upstream_busy", "message": "Mock upstream unavailable"}})
            return
        if self.path == "/v1/chat/completions":
            if payload.get("stream"):
                self.send_response(200)
                self.send_header("Content-Type", "text/event-stream")
                self.send_header("Cache-Control", "no-cache")
                self.end_headers()
                events = [
                    {"id": "chatcmpl_mock", "object": "chat.completion.chunk", "choices": [{"index": 0, "delta": {"content": "OK"}}]},
                    {"id": "chatcmpl_mock", "object": "chat.completion.chunk", "choices": [], "usage": {"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12, "prompt_tokens_details": {"cached_tokens": 3}}},
                ]
                for event in events:
                    self.wfile.write(("data: " + json.dumps(event) + "\n\n").encode())
                    self.wfile.flush()
                    time.sleep(0.02)
                self.wfile.write(b"data: [DONE]\n\n")
                self.wfile.flush()
                return
            self.send_json(200, {"id": "chatcmpl_mock", "object": "chat.completion", "model": "mock-gpt", "choices": [{"index": 0, "message": {"role": "assistant", "content": "OK"}, "finish_reason": "stop"}], "usage": {"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12, "prompt_tokens_details": {"cached_tokens": 3}}})
            return
        if self.path == "/v1/responses":
            self.send_json(200, {"id": "resp_mock", "object": "response", "status": "completed", "model": "mock-gpt", "output": [], "usage": {"input_tokens": 8, "output_tokens": 4, "total_tokens": 12, "input_tokens_details": {"cached_tokens": 2}}})
            return
        if self.path == "/v1/embeddings":
            self.send_json(200, {"object": "list", "model": "mock-gpt", "data": [{"object": "embedding", "index": 0, "embedding": [0.1, 0.2]}], "usage": {"prompt_tokens": 5, "total_tokens": 5}})
            return
        self.send_json(404, {"error": {"type": "not_found", "message": "Not found"}})


ThreadingHTTPServer(("0.0.0.0", 8090), Handler).serve_forever()
