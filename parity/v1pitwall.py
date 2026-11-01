"""v1's side of `parity pitwall` (paddock#133): what v1 1.0.0's pit wall data API
answers over a store v1's own `replay` wrote (README.md, "The pit wall").

The harness runs it in v1's own image, whose Python has v1 installed:

    python -c "$(cat v1pitwall.py)" --clock T [--clock T…] RECORDING [RECORDING…]

Each recording is replayed in turn by v1's `replay`, with its own --clock (the
n-th --clock is the n-th recording's), into
one temporary store, as `those-who-run replay RECORDING --store DIR --clock T`.
Then v1's pit wall app (pitwall/server.py's make_app, the app `those-who-run
pitwall` serves) is asked over HTTP, in process, for every data route: the
session list, and for each session the session, its laps, its same-track
sessions and, for every complete lap, its trace at 1500 points. Nothing in v1 is
changed or wrapped.

Each line printed is one answer: the route's name (`call`), the session id and
lap it was asked for, and v1's JSON body as it is.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import subprocess
import sys
import tempfile
from pathlib import Path

from aiohttp.test_utils import TestClient, TestServer

from those_who_run.pitwall.server import make_app

POINTS = 1500


def replay(store: Path, clock: str, recording: str) -> None:
    # v1's console lines go to stderr, so stdout holds only the answers.
    subprocess.run(
        ["those-who-run", "replay", recording, "--store", str(store), "--clock", clock],
        check=True,
        stdout=sys.stderr,
    )


def emit(call: str, body: object, session_id: str | None = None, lap: int | None = None) -> None:
    print(json.dumps({"call": call, "session_id": session_id, "lap": lap, "body": body}))


async def ask(store: Path) -> None:
    # No engineer: its routes aren't asked, and the data routes never call it.
    app = make_app(store, engineer_url="http://127.0.0.1:9", control_url="http://127.0.0.1:9")
    async with TestClient(TestServer(app)) as client:

        async def get(path: str) -> object:
            resp = await client.get(path)
            if resp.status != 200:
                raise SystemExit(f"v1 pitwall: GET {path}: {resp.status} {await resp.text()}")
            return await resp.json()

        listed = await get("/api/v1/sessions")
        emit("sessions", listed)
        for s in listed["sessions"]:
            sid = s["session_id"]
            emit("session", await get(f"/api/v1/sessions/{sid}"), sid)
            laps = await get(f"/api/v1/sessions/{sid}/laps")
            emit("laps", laps, sid)
            emit("same_track", await get(f"/api/v1/sessions/{sid}/same-track"), sid)
            for lap in laps["laps"]:
                if lap["complete"]:
                    n = lap["lap"]
                    body = await get(f"/api/v1/sessions/{sid}/laps/{n}/trace?points={POINTS}")
                    emit("trace", body, sid, n)


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--clock", action="append", required=True, help="each recording's clock, in order")
    p.add_argument("recordings", nargs="+")
    args = p.parse_args()
    if len(args.clock) != len(args.recordings):
        p.error("one --clock for each recording")
    with tempfile.TemporaryDirectory() as tmp:
        store = Path(tmp)
        for clock, recording in zip(args.clock, args.recordings, strict=True):
            replay(store, clock, recording)
        asyncio.run(ask(store))
    return 0


if __name__ == "__main__":
    sys.exit(main())
