"""Read a colored screen through an observing hc connection; never type or resize."""

import json
import queue
import re
import struct
import subprocess
import sys
import threading
import time
import unicodedata

ESCAPE = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[()][0-2A-Z]|\x1b[ -/]*[0-~]")


def capture(terminal):
    child = subprocess.Popen(["hc", "connect"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    watchdog = threading.Timer(4, child.kill)
    watchdog.daemon = True
    watchdog.start()
    try:
        child.stdin.write(b'{"jsonrpc":"2.0","id":0,"method":"hello","params":{"framed":true}}\n')
        child.stdin.flush()
        hello = json.loads(child.stdout.readline())
        if "error" in hello:
            raise RuntimeError(hello["error"])

        def send(channel, kind, payload):
            child.stdin.write(struct.pack("<IBI", channel, kind, len(payload)) + payload)
            child.stdin.flush()

        messages = queue.Queue()

        def read_exact(count):
            data = bytearray()
            while len(data) < count:
                part = child.stdout.read(count - len(data))
                if not part:
                    raise EOFError("Core closed the preview connection")
                data.extend(part)
            return bytes(data)

        def reader():
            try:
                while True:
                    channel, kind, size = struct.unpack("<IBI", read_exact(9))
                    if size > 16 * 1024 * 1024:
                        raise ValueError("Preview frame exceeds the protocol limit")
                    messages.put((channel, kind, read_exact(size)))
            except Exception as error:
                messages.put(error)

        threading.Thread(target=reader, daemon=True).start()
        # observe forbids input and resizing. The dimensions are required by the API
        # but ignored for observing viewers; the reply carries the actual size.
        send(0, 0, json.dumps({"jsonrpc": "2.0", "id": 1, "method": "terminal.attach", "params": {
            "terminal": terminal, "observe": True, "history": 0, "cols": 80, "rows": 24, "caller": None,
        }}).encode())
        channel = None
        cols, rows = 80, 24
        pending = []
        data = bytearray()
        deadline = time.monotonic() + 3
        while time.monotonic() < deadline:
            try:
                message = messages.get(timeout=min(0.05 if data else 3, max(0.001, deadline - time.monotonic())))
            except queue.Empty:
                if data:
                    break
                raise TimeoutError("No screen received from the core")
            if isinstance(message, Exception):
                if data:
                    break
                raise message
            ch, kind, payload = message
            if ch == 0 and kind == 0:
                reply = json.loads(payload)
                if "error" in reply:
                    raise RuntimeError(reply["error"].get("message", "Cannot observe terminal"))
                result = reply.get("result", {})
                channel = result.get("channel")
                info = result.get("terminal", {})
                cols, rows = info.get("cols", 80), info.get("rows", 24)
                pending, early = [], pending
                for frame in early:
                    messages.put(frame)
            elif channel is None:
                pending.append(message)
            elif ch == channel and kind == 0:
                if not data:
                    deadline = min(deadline, time.monotonic() + 0.15)
                data.extend(payload)
                send(channel, 1, struct.pack("<I", len(payload)))
                if len(data) > 1024 * 1024:
                    raise ValueError("Preview screen is too large")
            elif ch == channel and kind == 2:
                break
        if channel is not None:
            send(channel, 2, b"")
        if not data:
            raise RuntimeError("No screen received from the core")
        return decode_screen(data.decode("utf-8", "replace"), cols, rows)
    finally:
        watchdog.cancel()
        child.stdin.close()
        try:
            child.wait(timeout=0.3)
        except subprocess.TimeoutExpired:
            child.kill()
            child.wait()
        child.stdout.close()
        child.stderr.close()


def decode_screen(text, cols=80, rows=24):
    cols = max(1, min(int(cols), 500))
    rows = max(1, min(int(rows), 200, 40000 // cols))
    grid = [{} for _ in range(rows)]
    x = y = 0
    saved = (0, 0)
    style = {}

    def sgr(params):
        nonlocal style
        values = [int(v or 0) for v in params.split(";") if v.isdigit() or not v]
        if ":" in params:
            values = [int(v) for v in params.replace(":", ";").split(";") if v.isdigit()]
        at = 0
        while at < len(values):
            code = values[at]
            if code == 0:
                style = {}
            elif code in (1, 2, 3, 4, 7):
                style[{1: "bold", 2: "dim", 3: "italic", 4: "underline", 7: "reverse"}[code]] = True
            elif code in (22, 23, 24, 27):
                for key in {22: ("bold", "dim"), 23: ("italic",), 24: ("underline",), 27: ("reverse",)}[code]:
                    style.pop(key, None)
            elif 30 <= code <= 37 or 90 <= code <= 97:
                style["fg"] = code - (30 if code < 90 else 82)
            elif 40 <= code <= 47 or 100 <= code <= 107:
                style["bg"] = code - (40 if code < 100 else 92)
            elif code in (39, 49):
                style.pop("fg" if code == 39 else "bg", None)
            elif code in (38, 48) and at + 2 < len(values):
                key = "fg" if code == 38 else "bg"
                if values[at + 1] == 5:
                    style[key] = max(0, min(255, values[at + 2]))
                    at += 2
                elif values[at + 1] == 2 and at + 4 < len(values):
                    style[key] = "#" + "".join(f"{max(0, min(255, n)):02x}" for n in values[at + 2:at + 5])
                    at += 4
            at += 1

    def erase(row, start, end):
        if 0 <= row < rows:
            for col in range(max(0, start), min(cols, end)):
                grid[row][col] = (" ", dict(style))

    def literal(value):
        nonlocal x, y
        for char in value:
            if char == "\r":
                x = 0
            elif char == "\n":
                y += 1
            elif char == "\b":
                x = max(0, x - 1)
            elif char == "\t":
                x = min(cols, (x // 8 + 1) * 8)
            elif ord(char) >= 32 and char != "\x7f":
                if unicodedata.category(char) in ("Mn", "Me", "Cf"):
                    if 0 <= y < rows and x > 0:
                        col = x - 1
                        if grid[y].get(col, (None,))[0] == "":
                            col -= 1
                        before, old_style = grid[y].get(col, ("", dict(style)))
                        grid[y][col] = (before + char, old_style)
                    continue
                width = 2 if unicodedata.east_asian_width(char) in ("W", "F") else 1
                if x >= cols:
                    x, y = 0, y + 1
                if 0 <= y < rows:
                    grid[y][x] = (char, dict(style))
                    if width == 2 and x + 1 < cols:
                        grid[y][x + 1] = ("", dict(style))
                x += width

    at = 0
    for match in ESCAPE.finditer(text):
        literal(text[at:match.start()])
        control = match.group()
        if control.startswith("\x1b["):
            params, command = control[2:-1], control[-1]
            if command == "m":
                sgr(params)
            elif all(c in "0123456789;" for c in params):
                numbers = [int(n or 1) for n in params.split(";")]
                n = numbers[0]
                if command in ("H", "f"):
                    y, x = n - 1, (numbers[1] if len(numbers) > 1 else 1) - 1
                elif command == "G": x = n - 1
                elif command == "d": y = n - 1
                elif command == "A": y = max(0, y - n)
                elif command == "B": y = min(rows - 1, y + n)
                elif command == "C": x = min(cols - 1, x + n)
                elif command == "D": x = max(0, x - n)
                elif command == "K":
                    mode = int(params or 0)
                    erase(y, 0 if mode else x, x + 1 if mode == 1 else cols)
                elif command == "J":
                    mode = int(params or 0)
                    if mode in (2, 3):
                        grid = [{} for _ in range(rows)]
                    elif mode == 0:
                        erase(y, x, cols)
                        for row in range(y + 1, rows): erase(row, 0, cols)
                    elif mode == 1:
                        for row in range(y): erase(row, 0, cols)
                        erase(y, 0, x + 1)
                elif command == "s": saved = (x, y)
                elif command == "u": x, y = saved
        elif control == "\x1b7": saved = (x, y)
        elif control == "\x1b8": x, y = saved
        at = match.end()
    literal(text[at:])
    output = []
    for row in grid:
        runs = []
        last = max((col for col, (char, fmt) in row.items() if char.strip() or fmt.get("bg") is not None or fmt.get("reverse")), default=-1)
        for col in range(last + 1):
            char, fmt = row.get(col, (" ", {}))
            if not char:
                continue
            if runs and runs[-1]["style"] == fmt:
                runs[-1]["text"] += char
            else:
                runs.append({"text": char, "style": fmt})
        output.append(runs)
    while output and not output[-1]:
        output.pop()
    return {"rows": output, "cols": cols}


if __name__ == "__main__":
    try:
        print(json.dumps(capture(sys.argv[1]), ensure_ascii=False))
    except Exception as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
