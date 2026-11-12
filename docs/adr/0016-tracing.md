# 0016: Tracing across the bus

**Status:** Proposed. Waits for the maintainer's decision on its pull request,
which also settles "tracing now or not" (#102, open question).

## Context

v1 decided tracing was overkill because the engineer was one process
(those-who-run/those-who-run#345), and named what would reopen it: a second
process on the callout path. v2 is that. A callout starts as a frame in `ecu`
(Go), travels as an event over NATS, and is taken, synthesised and played by
`radio` (Python). `radio` and the card subscriber would each invent their own
answer if none is written.

What exists or is planned without traces:

- the callout id carried from `ecu` to `radio` on every log line (#99);
- `created_at` and `expires_at` on the callout message;
- per-stage histograms in `radio`.

The frame path has a budget of 16.7 ms (one frame at 60 Hz, #100).

### What a span costs

Measured on 2026-11-12 on one machine (Intel Core i5-8500T, 2.10 GHz, Linux
amd64, Go 1.27.4, CPython 3.13.5, OpenTelemetry 1.47.0 for Go and 1.45.2 for
Python, the latest releases that day). The programs were throwaway, outside
the repository; none of this is `ecu`'s code, so the figure is for the
library call, not for `ecu`'s frame path itself.

| What | Go | Python |
|---|---|---|
| Start and end one span, API only, no SDK installed (no-op) | 139 ns, 3 allocs, 240 B | 4.7 µs |
| Same, SDK installed, sampler off | 195 ns, 2 allocs | 6.7 µs |
| Same, SDK installed, always sampled, no exporter | 391 ns, 2 allocs | not measured |
| Inject `traceparent` into a header map | 260 ns, 3 allocs | not measured |

How: Go, `go test -bench` with `b.ReportAllocs()`, `-count 5` for the no-op
(spread under 1%) and `-count 3` for the rest, a tracer from
`otel.Tracer`, one `Start` and `End` per iteration. Python, `timeit`, best of
3 runs of 500 000 calls (200 000 for the SDK case) of
`with tracer.start_as_current_span(...)`; an empty function costs 0.03 µs on
the same machine, so the rest is the span. Against the 16.7 ms budget, one
no-op Go span is 0.0008%; three spans on a frame, about 0.4 µs, are 0.0025%.
Python spans are not on a per-frame path: `radio` handles callouts, a few per
lap.

### What it costs in an image

Measured by building and installing, on the same machine:

| Image | Without | With the API only | With SDK and OTLP exporter |
|---|---|---|---|
| Go binary, stripped (`-ldflags "-s -w"`) | 1.2 MB (a hello world) | 4.9 MB | 14.7 MB (OTLP over gRPC) |
| Python site-packages | 12 MB (a bare venv) | 13 MB | 25 MB (SDK and OTLP over HTTP) |

The Go baseline is a hello world, not `ecu`, and the API figure includes the
packages the library pulls in; the sizes show the order, not `ecu`'s exact
delta. The exporter, not the API, is what costs: about 10 MB of binary in Go
and 12 MB of packages in Python, and each adds dependencies to keep up to date
(#102 asks that a new direct dependency be a proposal).

## Decision

### 1. Propagate W3C trace context in NATS headers: reserved, not emitted yet

`traceparent` (and `tracestate` when present) is the one way to join the
pieces, in NATS headers beside the CloudEvents headers
([ADR 0002](0002-contracts.md)), never in the JSON body. The name is reserved
now so that nothing else uses it. Ids in headers of our own, and shared log
fields alone, are not chosen: they would be a private copy of what
`traceparent` already is.

Reserved log fields: `trace_id` and `span_id`, the W3C hex forms. They go on a
line only when a trace is active, and never replace the callout id.

Settles: #102 (R2, propagation).

### 2. Off by default, and nothing is instrumented in v2's first releases

Tracing is off in every component unless its configuration turns it on.
Instrumentation is written against the API only, so that off is the no-op
above and an image carries no SDK or exporter. A build that wants export is a
choice made later, by the point 5 triggers. A component that receives a
`traceparent` it does not trace passes it on unchanged where it republishes,
and ignores it otherwise.

Settles: #102 (R2, default).

### 3. The spans, when on

One trace per callout, five spans, named for the stage:

| Span | Where | Starts and ends |
|---|---|---|
| `frame.process` | `ecu` | the frame that raised the callout, through the rules |
| `callout.publish` | `ecu` | the publish to NATS; its context goes in `traceparent` |
| `callout.take` | `radio` | from the message's arrival to the decision to speak or drop (expired) |
| `callout.synthesise` | `radio` | text to audio |
| `callout.play` | `radio` | audio sent to voice, to its end |

`frame.process` is not a span on every frame: at 60 Hz that is 60 root spans a
second, almost all of no interest. `ecu` records it only for a frame that
raised a callout, with that frame's start time as the span's start. Span
attributes carry the callout id, so a trace can be found from a log line and
the reverse.

Settles: #102 (R2, spans).

### 4. The exporter is OTLP, and v2 ships no collector or trace store

When on, the exporter is OTLP, to an endpoint set by the standard
`OTEL_EXPORTER_OTLP_ENDPOINT` variable. v2 includes no collector, Tempo or
Jaeger in `deploy/`: nobody runs them in the cluster today, and one maintainer
would be keeping a stateful service for a question the logs already answer.
Someone who wants traces points the endpoint at their own.

Settles: #102 (R2, exporter and store).

### 5. What is enough without traces, and what would change this

Enough, and what v2 relies on:

1. the callout id on every log line, from `ecu` to `radio` (#99): one `grep`
   follows a callout across both;
2. `created_at` and `expires_at` on the message: the age at each stage is
   `now - created_at`, and a late callout is dropped by `expires_at`;
3. `radio`'s per-stage histograms: where time goes in aggregate, which a trace
   of one callout does not show.

Together they answer "was this callout late, and in which stage". They do not
give one picture across processes with a timeline.

What would reopen this decision:

- a third process on the callout path, or a fan-out where one callout feeds
  several subscribers (the card subscriber included) and their latency has to
  be compared;
- a lateness the ids, ages and histograms can not place, found twice;
- someone running a collector who wants v2's traces, which costs the
  exporter's image growth above;
- a measurement on `ecu`'s real frame path showing the no-op cost is not
  negligible, which these figures do not predict.

Settles: #102 (R3, R4).

## Consequences

- **Nothing to build for tracing now.** `radio` (Brundle) and the card
  subscriber log the callout id and use no `traceparent`.
- **The header and fields are taken.** A later component can not use
  `traceparent`, `trace_id` or `span_id` for anything else.
- **Turning tracing on is a follow-up per component:** API instrumentation,
  an opt-in build with the exporter, and a test that a `traceparent` crosses
  `ecu` to `radio`. Those are not filed by this record; the maintainer's
  decision on it says whether they are, and #105 (blocked by this record) is
  unblocked on whichever way it goes.
- **The costs are on the page.** A reader can see that the API alone is cheap
  in time and that the exporter is what grows an image.
- **The measurement is a library figure, from one machine.** It is not `ecu`'s
  frame path (see Context).

## Alternatives considered

- **Trace now, with an exporter on by default.** About 10 MB per Go image and
  12 MB per Python one, a new endpoint to configure, and a collector to run,
  for lateness the ids and histograms already locate.
- **Ids in our own headers, or log fields alone.** Cheaper to start and
  incompatible with every tracing tool later.
- **Do not reserve anything.** Then `radio` and the card subscriber each
  invent a header, which is what this record exists to stop.
