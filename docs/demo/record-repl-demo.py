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
COLS = int(os.environ.get("DEMO_COLS", "150"))
TYPE_DELAY = float(os.environ.get("DEMO_TYPE_DELAY", "0.065"))
COMMAND_HOLD = float(os.environ.get("DEMO_COMMAND_HOLD", "0.8"))
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


def forward_output(fd, transcript, timeout):
    deadline = time.monotonic() + timeout
    while True:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            return
        ready, _, _ = select.select([fd], [], [], remaining)
        if not ready:
            return
        try:
            chunk = os.read(fd, 8192)
        except OSError as error:
            if error.errno == errno.EIO:
                return
            raise
        if not chunk:
            return
        os.write(sys.stdout.fileno(), chunk)
        transcript.extend(chunk)


def command_is_visible(transcript, command):
    plain = ANSI.sub(b"", transcript).decode("utf-8", "replace").replace("\r", "")
    return plain.rstrip().endswith("> " + command)


def send_command(fd, command):
    transcript = bytearray()
    for character in command:
        os.write(fd, character.encode("utf-8"))
        forward_output(fd, transcript, TYPE_DELAY)
        time.sleep(TYPE_DELAY)
    deadline = time.monotonic() + 2
    while not command_is_visible(transcript, command) and time.monotonic() < deadline:
        forward_output(fd, transcript, 0.05)
    if not command_is_visible(transcript, command):
        raise RuntimeError(f"typed command was not visibly echoed before submission: {command!r}")

    hold_until = time.monotonic() + COMMAND_HOLD
    while time.monotonic() < hold_until:
        forward_output(fd, transcript, min(0.05, hold_until - time.monotonic()))
    os.write(fd, b"\r")


def clear_view(fd):
    """Use the REPL's documented Ctrl-L clear before the next view."""
    os.write(fd, b"\x0c")
    read_until_prompt(fd, bytearray())


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
        time.sleep(2)

        transcript = bytearray()
        send_command(fd, "/projects")
        read_until_prompt(fd, transcript, b"PROJECT")
        time.sleep(2.2)

        project_row = row_number(transcript, "Ledger Studio")
        clear_view(fd)
        transcript = bytearray()
        send_command(fd, f"/project {project_row}")
        read_until_prompt(fd, transcript, b"Ledger Studio")
        time.sleep(2.2)

        clear_view(fd)
        transcript = bytearray()
        send_command(fd, "/sessions")
        read_until_prompt(fd, transcript, b"SESSION")
        time.sleep(2.8)

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
