#!/usr/bin/env python3
"""Record actual interactive navigation through the isolated demo database."""

import errno
import fcntl
import json
import os
import pty
import re
import select
import signal
import struct
import subprocess
import sys
import termios
import time


CLI = os.environ.get("CLI_BIN", "komposyn-lg")
ROWS = int(os.environ.get("DEMO_ROWS", "32"))
COLS = int(os.environ.get("DEMO_COLS", "100"))
ANSI = re.compile(rb"\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))")


def read_until_prompt(fd, transcript, expected=None, timeout=10):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        ready, _, _ = select.select([fd], [], [], max(0, deadline - time.monotonic()))
        if not ready:
            break
        try:
            chunk = os.read(fd, 8192)
        except OSError as error:
            if error.errno == errno.EIO:
                break
            raise
        if not chunk:
            break
        os.write(sys.stdout.fileno(), chunk)
        transcript.extend(chunk)
        has_expected = expected is None or expected in transcript
        if has_expected and (transcript.endswith(b"> ") or transcript.endswith(b">  \x08")):
            time.sleep(0.6)
            return bytes(transcript)
    raise RuntimeError("interactive prompt did not appear")


def row_number(transcript, label):
    plain = ANSI.sub(b"", transcript).decode("utf-8", "replace")
    pattern = re.compile(r"^\s*(\d+)\s+" + re.escape(label) + r"(?:\s|$)", re.MULTILINE)
    match = pattern.search(plain)
    if not match:
        raise RuntimeError(f"could not select {label!r} from the displayed table:\n{plain}")
    return match.group(1)


def send_command(fd, command):
    os.write(fd, command.encode("utf-8") + b"\r")


def main():
    data_dir = os.environ.get("TOKEN_LOGGER_DATA_DIR")
    if not data_dir:
        raise SystemExit("TOKEN_LOGGER_DATA_DIR must point to the isolated synthetic fixture")
    if not os.path.isfile(os.path.join(data_dir, "token-logger.db")):
        raise SystemExit("the isolated synthetic fixture database is missing")
    with open(os.path.join(data_dir, "settings.json"), encoding="utf-8") as settings_file:
        settings = json.load(settings_file)
    if not settings.get("onboarding_complete") or settings.get("collector_enabled") or settings.get("auto_start"):
        raise SystemExit("complete onboarding and keep collection and auto-start disabled for the capture")
    version = subprocess.run([CLI, "version"], check=True, capture_output=True, text=True).stdout.strip()
    if version != "komposyn-lg 0.9.0 (core 0.8.0)":
        raise SystemExit(f"expected signed CLI v0.9.0 with core v0.8.0; got {version!r}")
    pid, fd = pty.fork()
    if pid == 0:
        os.execvpe(CLI, [CLI], os.environ.copy())

    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
    try:
        transcript = bytearray()
        read_until_prompt(fd, transcript)
        sequence = ["/projects"]
        for command in sequence:
            transcript = bytearray()
            send_command(fd, command)
            read_until_prompt(fd, transcript, b"PROJECT")

        project_row = row_number(transcript, "Ledger Studio")
        transcript = bytearray()
        send_command(fd, f"/project {project_row}")
        read_until_prompt(fd, transcript, b"Ledger Studio")

        transcript = bytearray()
        send_command(fd, "/sessions")
        read_until_prompt(fd, transcript, b"SESSION")
        session_row = row_number(transcript, "session-ledger-codex")

        transcript = bytearray()
        send_command(fd, f"/session {session_row}")
        read_until_prompt(fd, transcript, b"MODEL")
        time.sleep(0.8)

        transcript = bytearray()
        send_command(fd, "/clear")
        read_until_prompt(fd, transcript)

        transcript = bytearray()
        send_command(fd, "/models")
        read_until_prompt(fd, transcript, b"MODEL")

        time.sleep(1.5)
        send_command(fd, "/quit")
        _, status = os.waitpid(pid, 0)
        if not os.WIFEXITED(status) or os.WEXITSTATUS(status) != 0:
            raise RuntimeError(f"komposyn-lg exited unexpectedly: wait status {status}")
    finally:
        try:
            os.close(fd)
        except OSError:
            pass
        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            pass


if __name__ == "__main__":
    main()
