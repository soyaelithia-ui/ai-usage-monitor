#!/usr/bin/env python3
import sys
import json
import os
import urllib.request
from pathlib import Path

def main():
    # Read stdin payload
    raw_input = ""
    try:
        raw_input = sys.stdin.read()
    except Exception:
        pass

    payload = {}
    if raw_input.strip():
        try:
            payload = json.loads(raw_input)
        except Exception:
            pass

    # Determine event type and write output immediately
    is_stop = "terminationReason" in payload or os.environ.get("ANTIGRAVITY_EVENT") == "Stop"
    if is_stop:
        sys.stdout.write('{"decision":""}\n')
    else:
        sys.stdout.write('{}\n')
    sys.stdout.flush()

    # Process usage reporting in background
    try:
        report_usage(payload)
    except Exception:
        pass

def report_usage(payload):
    conv_id = payload.get("conversationId", "unknown")
    transcript_path = payload.get("transcriptPath")

    if not transcript_path or not os.path.exists(transcript_path):
        brain_dir = Path(os.environ.get("ANTIGRAVITY_BRAIN_DIR", Path.home() / ".gemini/antigravity-cli/brain"))
        candidate = brain_dir / conv_id / ".system_generated/logs/transcript.jsonl"
        if candidate.exists():
            transcript_path = str(candidate)

    if not transcript_path or not os.path.exists(transcript_path):
        return

    # Cache file for steps already sent in this workspace
    cache_dir = os.environ.get("PULSE_DATA_DIR", str(Path(__file__).resolve().parents[1] / "data"))
    os.makedirs(cache_dir, exist_ok=True)
    cache_file = os.path.join(cache_dir, ".sent_steps")
    sent_steps = set()
    if os.path.exists(cache_file):
        try:
            with open(cache_file, "r") as f:
                sent_steps = set(line.strip() for line in f if line.strip())
        except Exception:
            pass

    new_sent = []
    endpoint = "http://127.0.0.1:3000/api/usage"

    with open(transcript_path, "r", encoding="utf-8") as f:
        for line in f:
            if not line.strip():
                continue
            try:
                entry = json.loads(line)
            except Exception:
                continue

            if entry.get("type") != "PLANNER_RESPONSE":
                continue

            step_idx = entry.get("step_index")
            if step_idx is None:
                continue

            req_id = f"agy-{conv_id}-{step_idx}"
            if req_id in sent_steps:
                continue

            inp_tokens = (entry.get("input_tokens") or 0) + (entry.get("cache_read_tokens") or 0)
            out_tokens = entry.get("output_tokens") or 0

            body = json.dumps({
                "agent_name": "Antigravity",
                "model": "gemini-3.8-flash",
                "prompt_tokens": inp_tokens,
                "completion_tokens": out_tokens,
                "request_id": req_id
            }).encode("utf-8")

            req = urllib.request.Request(
                endpoint,
                data=body,
                headers={"Content-Type": "application/json"}
            )
            try:
                with urllib.request.urlopen(req, timeout=2.0) as resp:
                    if resp.status in (200, 201):
                        new_sent.append(req_id)
            except Exception:
                # If local server is unreachable, stop trying for this run
                break

    if new_sent:
        try:
            with open(cache_file, "a", encoding="utf-8") as f:
                for s in new_sent:
                    f.write(s + "\n")
        except Exception:
            pass

if __name__ == "__main__":
    main()
