#!/usr/bin/env python3
"""Stub OpenAI-compatible LLM for the /v1 wire smoke.

The eino agenticopenai client speaks the Chat Completions API with
stream:true. Scripted by the last user message text:

  "APPROVE"     -> tool call to `execute` with a dangerous command (HITL path)
  "TOOLRUN"     -> tool call to `execute` with a safe command
  "DELEGATE"    -> tool call to `agent` (foreground delegation)
  "BGDELEGATE"  -> tool call to `agent` with run_in_background=true
  "BGSHELL"     -> tool call to `execute` with run_in_background=true
  "SUBBRIEF"    -> plain child report (the sub-agent's fresh conversation)
  otherwise     -> plain assistant text

When the conversation already carries a role=="tool" message (the tool ran),
the stub phases on that result: a background launch ("running in background
with ID: ...") polls task_output with the extracted id, a task_output record
keys the final answer off the child/shell output it carries, a foreground
child report becomes the parent's final answer, and everything else gets the
post-tool assistant text. promptgen calls arrive without those markers and
receive the identity/soul JSON contract.

Phases key on the LAST message only: the full replayed history may contain
older tool results and older user markers, but a continuation turn always
ends with the newest message. The background final answers stream in small
drip chunks so the parent turn stays alive past the completion pump's poll
interval — the pump leases the terminal notification within one tick of the
task completing, and the drip guarantees that tick happens while the run can
still append the x.task_completed chip.

The stub also records the model-facing tool surface of every chat/completions
call in a capped ring and serves it on GET /debug/calls — the smoke suite
reads it right after a probe turn settles to pin the v1 request-narrowing
contract (a request may narrow the agent's effective tool set, never extend
it, and tool_choice "none" strips it). The ring matters: a turn's trailing
memory-curation model call rides tools-less and would clobber a
last-call-only record before the suite polls it, so the suite reads the last
NON-EMPTY entry — the turn's main model call.
"""
import json
import re
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

TEXT = "stub says hi from wire smoke"
POST_TOOL_TEXT = "stub finished after tool"
GEN_TEXT = json.dumps({
    "identity": "Atlas is the OnClaw wire-smoke runner.",
    "soul": "Terse, precise, and helpful.",
})

# Delegation scenario copy (smoke.sh section 32): stable markers the shell
# suite greps for and the stub phases on. The child brief must not embed any
# other phase marker, and the child reply is what the parent's tool result
# carries — both facts the phasing below leans on.
CHILD_BRIEF = "SUBBRIEF research the stub delegation scenario and report a one-line finding."
CHILD_REPORT = "SUBRESULT stub child report complete"
DELEGATE_FINAL = "DELEGATEFINAL stub parent collected the child report"
BGDELEGATE_FINAL = "BGFINAL stub parent collected the background child report"
BGSHELL_COMMAND = "echo onclaw-bg-shell-ok"
BGSHELL_FINAL = "BGSHELLFINAL stub parent collected the background command output"

# Background launch copies (eino subagent + fs shell lanes) and the task_output
# record header; the id grammar is <kind>_<base64url> per adk/backgroundtask.
LAUNCH_ID_RE = re.compile(r"running in background with ID: ([A-Za-z0-9_-]+)\.")
TASK_RECORD_ID_RE = re.compile(r"^Task ID: ([A-Za-z0-9_-]+)$", re.MULTILINE)

# Model-facing tool surface per chat/completions call, capped ring: the smoke
# suite polls GET /debug/calls immediately after a probe turn settles and
# reads the LAST NON-EMPTY entry — the turn's main model call. A turn's
# trailing memory-curation call rides tools-less (it would poison a
# last-call-only record), so emptiness is exactly how the suite tells them
# apart.
LAST_CALLS = []
LAST_CALLS_CAP = 64


def tool_names(body):
    """Flatten a Chat Completions tools array to its function names."""
    names = []
    for t in body.get("tools") or []:
        if isinstance(t, dict):
            fn = t.get("function") if isinstance(t.get("function"), dict) else t
            if isinstance(fn, dict) and fn.get("name"):
                names.append(fn["name"])
    return names


def record_call(body):
    LAST_CALLS.append(tool_names(body))
    del LAST_CALLS[:-LAST_CALLS_CAP]


def content_text(content):
    """Flatten a message content (string, typed-block list, or anything) to text."""
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts = []
        for p in content:
            if isinstance(p, dict):
                parts.append(str(p.get("text", "")))
            else:
                parts.append(str(p))
        return " ".join(parts)
    if content is None:
        return ""
    return json.dumps(content)


def last_user_text(messages):
    out = ""
    for m in messages:
        if m.get("role") == "user":
            out = content_text(m.get("content"))
    return out


def last_tool_text(messages):
    for m in reversed(messages or []):
        if m.get("role") == "tool":
            return content_text(m.get("content"))
    return ""


def delegate_tool_call(run_in_background=False):
    args = {"subagent_type": "general-purpose", "prompt": CHILD_BRIEF,
            "description": "Stub background delegation" if run_in_background else "Stub delegation"}
    if run_in_background:
        args["run_in_background"] = True
    return {"kind": "tool", "name": "agent", "arguments": json.dumps(args)}


def task_output_call(task_id):
    return {"kind": "tool", "name": "task_output",
            "arguments": json.dumps({"task_id": task_id})}


def tool_result_reply(messages):
    """Phase on the tool result the conversation ends with."""
    text = last_tool_text(messages)
    # A task_output record (header "Task ID: ..." + "Status: ..."): a
    # non-terminal status polls again; a terminal one keys the final answer
    # off the output the record carries.
    if "Task ID:" in text and "Status:" in text:
        task_id = TASK_RECORD_ID_RE.search(text)
        if re.search(r"^Status: (running|pending)$", text, re.MULTILINE) and task_id:
            return task_output_call(task_id.group(1))
        if "Status: completed" in text:
            if CHILD_REPORT in text:
                return {"kind": "text", "text": BGDELEGATE_FINAL, "drip": True}
            if "onclaw-bg-shell-ok" in text:
                return {"kind": "text", "text": BGSHELL_FINAL, "drip": True}
    # A background launch (agent or shell lane): poll the freshly minted id.
    launch = LAUNCH_ID_RE.search(text)
    if launch:
        return task_output_call(launch.group(1))
    # Foreground delegation: the tool result IS the child's final report.
    if CHILD_REPORT in text:
        return {"kind": "text", "text": DELEGATE_FINAL}
    return {"kind": "text", "text": POST_TOOL_TEXT}


def scripted_reply(messages):
    # Phase on the LAST message only: the full replayed history may contain
    # older tool results, but a continuation turn always ends with one.
    msgs = [m for m in (messages or []) if m.get("role") in ("user", "tool", "assistant")]
    if msgs and msgs[-1].get("role") == "tool":
        return tool_result_reply(messages)
    text = last_user_text(messages)
    if "SUBBRIEF" in text:
        # The sub-agent's fresh conversation: its only user message is the
        # delegation brief, so one plain reply finishes the child.
        return {"kind": "text", "text": CHILD_REPORT}
    if "APPROVE" in text:
        # A unique command per run: the durable approval ledger remembers
        # previously-approved commands, so a fixed one would auto-approve.
        return {"kind": "tool", "name": "execute",
                "arguments": json.dumps({"command": "rm -rf ./v1smoke-target-" + uuid.uuid4().hex[:8]})}
    if "TOOLRUN" in text:
        return {"kind": "tool", "name": "execute",
                "arguments": json.dumps({"command": "echo wire-smoke-tool-ok"})}
    if "BGDELEGATE" in text:
        return delegate_tool_call(run_in_background=True)
    if "BGSHELL" in text:
        return {"kind": "tool", "name": "execute",
                "arguments": json.dumps({"command": BGSHELL_COMMAND, "run_in_background": True})}
    if "DELEGATE" in text:
        return delegate_tool_call()
    if "identity" in text.lower() and "soul" in text.lower():
        return {"kind": "text", "text": GEN_TEXT}
    return {"kind": "text", "text": TEXT}


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path.endswith("/debug/calls"):
            return self.json({"calls": LAST_CALLS})
        self.send_error(404)

    def do_POST(self):
        n = int(self.headers.get("Content-Length", 0))
        body = json.loads(self.rfile.read(n) or b"{}")
        if self.path.endswith("/chat/completions"):
            return self.chat_completions(body)
        self.send_error(404)

    def chat_completions(self, body):
        record_call(body)
        reply = scripted_reply(body.get("messages") or [])
        cid = "chatcmpl_" + uuid.uuid4().hex[:10]
        model = body.get("model", "stub-1")

        if not body.get("stream"):
            if reply["kind"] == "tool":
                msg = {"role": "assistant", "content": None,
                       "tool_calls": [{"id": "call_" + uuid.uuid4().hex[:8], "type": "function",
                                       "function": {"name": reply["name"], "arguments": reply["arguments"]}}]}
            else:
                msg = {"role": "assistant", "content": reply["text"]}
            return self.json({
                "id": cid, "object": "chat.completion", "created": 0, "model": model,
                "choices": [{"index": 0, "finish_reason": "stop", "message": msg}],
                "usage": {"prompt_tokens": 12, "completion_tokens": 9, "total_tokens": 21},
            })

        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()

        def chunk(delta, finish=None, usage=None):
            payload = {"id": cid, "object": "chat.completion.chunk", "created": 0, "model": model,
                       "choices": [{"index": 0, "delta": delta, "finish_reason": finish}]}
            if usage is not None:
                payload["usage"] = usage
            self.wfile.write(f"data: {json.dumps(payload)}\n\n".encode())
            self.wfile.flush()

        chunk({"role": "assistant"})
        if reply["kind"] == "tool":
            chunk({"tool_calls": [{"index": 0, "id": "call_" + uuid.uuid4().hex[:8], "type": "function",
                                   "function": {"name": reply["name"], "arguments": ""}}]})
            chunk({"tool_calls": [{"index": 0, "function": {"arguments": reply["arguments"]}}]})
        else:
            text = reply["text"]
            # Drip mode (background final answers): tiny chunks with sleeps
            # keep the parent turn alive for the completion pump's tick — see
            # the module docstring.
            size = 2 if reply.get("drip") else 8
            for i in range(0, len(text), size):
                chunk({"content": text[i:i + size]})
                if reply.get("drip"):
                    time.sleep(0.02)
        chunk({}, finish="stop", usage={"prompt_tokens": 12, "completion_tokens": 9, "total_tokens": 21})
        self.wfile.write(b"data: [DONE]\n\n")
        self.wfile.flush()

    def json(self, payload):
        data = json.dumps(payload).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


if __name__ == "__main__":
    import sys
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 9147
    # ThreadingHTTPServer: the delegation lanes put the parent's and the
    # child's model calls on the wire concurrently; serialized serving would
    # still be deadlock-free but threaded serving keeps the lanes untangled.
    ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
