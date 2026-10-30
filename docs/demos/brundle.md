# The Brundle demo

The Brundle demo (paddock#64): a v1 recording replayed onto the bus is spoken by
`radio`, and what `radio` says matches what v1 says for the same recording,
callout for callout. Then the same race is heard in Discord.

It has two parts:

- **The scripted check**, which CI runs: `make brundle-demo`
  ([`parity spoken`](../../parity/README.md#what-is-spoken)) diffs `radio`'s
  console against v1's `replay`, both with `script: false`, on the nine
  recordings below (R1 to R3).
- **The live listen**, by hand on the laptop (R4, AC7): a replay at recorded
  speed spoken in a Discord voice channel and heard on a phone or the headset.
  Replays never run on the cluster (decided 2026-10-25), so this runs on the
  laptop's compose stack. [The steps](#the-live-listen) are below.

## The scripted check

```sh
make brundle-demo            # the nine below
make -C parity spoken RECORDINGS=real-nuc-sugo-two-stops-2026-10-18
```

It needs what `make parity` needs: Go, docker with compose v2 (or `CONTAINER`
and `COMPOSE` naming others), and `127.0.0.1:4222`, `:8222` and `:8080` free.
The output is each recording's differences, then a table and a pass or fail.

| AC | Recording | What v1 says (`script: false`) |
|---|---|---|
| AC1 | `real-nuc-suzuka-no-stop-2026-10-15` | 23 callouts, a clean race on the last litre |
| AC2 | `real-nuc-suzuka-pit-stop-2026-10-15` | 15 callouts, one stop |
| AC2 | `real-nuc-gilles-villeneuve-pit-stop-2026-10-17` | 11 callouts, one stop |
| AC3 | `real-nuc-sugo-two-stops-2026-10-18` | 52 callouts: "Down to eighth." 0.7 s before the first entry is dropped, and each "In lap, …" is held to its exit |
| AC4 | `real-nuc-fuji-timed-2026-10-15` | 4 callouts, a time trial |
| AC4 | `real-nuc-suzuka-timed-race-2026-10-16` | 16 callouts, a timed race |
| AC5 | `real-nuc-suzuka-joined-mid-race-2026-10-18` | 2 callouts: "Back with you. Lap seven. P one." and a lap time, nothing about the grid |
| AC6 | `real-nuc-sugo-replay-2026-10-18` | nothing |
| AC6 | `real-nuc-laguna-seca-demo-2026-10-18` | nothing |

The counts are v1 1.0.0's `those-who-run replay` with `GT7_RULES_FILE` naming a
file of `script: false`. An equal list carries each AC's own condition with it:
v1 says only fuel between each entry and exit, holds the in-lap call to the exit
and drops the place lost before the first entry, so a `radio` that says what v1
says does too.

Known differences, with the decision behind each, are in
[`parity/known.go`](../../parity/known.go).

## The live listen

R4 and AC7, for the maintainer to run on the laptop, with v1's bot: since
2026-10-29 v2 is the live engineer and v1's engineer is retired, so v1's bot
is free. One bot token is one voice connection, so nothing else may be speaking
as it meanwhile.

1. In a paddock checkout at `main`, start from nothing:

   ```sh
   make reset
   make up
   ```

2. Outside the checkout (it holds no secret), write a compose file laid over the
   stack's that points `radio` at Discord, with the timeline (#60 R5) in the WAV
   directory:

   ```yaml
   # ~/twr-brundle.yaml
   services:
     radio:
       environment:
         TWR_RADIO_SINK: discord
         TWR_RADIO_TIMING: simulated
         TWR_RADIO_TIMELINE_FILE: /wav/timeline.jsonl
         TWR_DISCORD_BOT_TOKEN: ${TWR_DISCORD_BOT_TOKEN:?}
         TWR_DISCORD_VOICE_CHANNEL_ID: ${TWR_DISCORD_VOICE_CHANNEL_ID:?}
   ```

3. With v1's bot token and the voice channel's id in the shell's environment
   (`TWR_DISCORD_BOT_TOKEN`, `TWR_DISCORD_VOICE_CHANNEL_ID`, from the password
   manager, never a file in the repository), start `radio` on the run's source:

   ```sh
   mkdir -p .cache/radio-wav
   TWR_UID=$(id -u) TWR_GID=$(id -g) TWR_RADIO_SRC=replay-brundle \
     TWR_RADIO_WAV=$PWD/.cache/radio-wav \
     docker compose -p twr -f deploy/compose/compose.yaml -f ~/twr-brundle.yaml \
     --profile replay up -d radio
   docker compose -p twr -f deploy/compose/compose.yaml logs -f radio
   ```

   Under rootless podman, `TWR_UID=0 TWR_GID=0` instead, as `make radio` does.
   Wait for `callouts: taking` in the log.

4. Join the voice channel on the phone or the headset, then replay the two-stop
   race at recorded speed (about 18 minutes):

   ```sh
   make replay FILE=real-nuc-sugo-two-stops-2026-10-18.jsonl.gz SPEED=1 RUN=brundle
   ```

   The bot joins on the first callout. Listen for the AC3 points: the place lost
   before the first entry is not said, nothing but fuel in the lane, and each
   in-lap call comes at the exit.

5. Write it up in the PR that closes #64: what was heard, and the timeline,
   `.cache/radio-wav/timeline.jsonl` (each callout's queued, started and
   finished times, or its drop and the stage). What to hear, in order, is v1's
   list from `cd parity && go run . spoken --keep real-nuc-sugo-two-stops-2026-10-18`.
   With the script on, as here, the words may differ from that list, but not the
   callouts or their order.

6. Stop it:

   ```sh
   make down
   ```

## Results

The scripted check's results are recorded in the PR that added it, from CI's
`parity-spoken` job.
