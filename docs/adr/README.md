# Decision records

An ADR (architecture decision record) writes down one decision, why it was
made and what follows from it, so the issues that depend on it stop deciding
it again, each a little differently.

v2's records live here, in `paddock`, and nowhere else: the component
repositories hold code, not decisions. The process is v1's, carried over
unchanged.

## When to write one

Write an ADR when **more than one issue depends on the same decision**: how
events travel, who owns the data volume, how a component's repository is
laid out. A decision that only one issue needs stays in that issue (its
Approach or Open questions).

Signs it is time:

- two specs recommend different answers to the same question;
- a spec's Open questions say "decide in #…" or "record it somewhere";
- a requirement in one issue breaks a requirement in another.

## How

- One file per record: `NNNN-short-name.md`, numbered in order. A number is
  never reused, not even for a record that was dropped.
- Sections: **Status**, **Context**, **Decision** (one heading per point, each
  naming the issues it settles), **Consequences**, and what it means for the
  issue that asked for it.
- Status is *Accepted* or *Superseded by NNNN*. A record is not edited to say
  something new: a new record replaces it, and the old one points to it.
- Every issue a record settles links back to it, and its body agrees with it.
  Where a test can back a decision, the record names the test.
- Plain and short, like the rest of the docs (see
  [`../ISSUE_FORMAT.md`](../ISSUE_FORMAT.md), "Writing").

## Records

| ADR | Decision |
|---|---|
| [0001](0001-v2-architecture.md) | The v2 architecture: events over NATS (design B), the subject scheme, who owns the session and the rules, one repository per component, the languages, the dash reading NATS directly, replays and parity. |

## v1's records

v1's records stay in v1's repository and are linked here, not copied, so
there is one copy of each. Each row says what still holds in v2, and which v2
record carries it on or replaces it.

| v1 ADR | What still holds in v2 |
|---|---|
| [0001, data volume and network](https://github.com/those-who-run/those-who-run/blob/main/docs/adr/0001-data-and-network.md) | The one writer on the data volume carries into ADR 0003 (#9). The rest (ports, secrets, images, Postgres) is v1's. |
| [0002, the outing archive](https://github.com/those-who-run/those-who-run/blob/main/docs/adr/0002-outing-archive.md) | Still where archived recordings live. v2's fixtures come from it. |
| [0003, UI stack](https://github.com/those-who-run/those-who-run/blob/main/docs/adr/0003-ui-stack.md) | One origin holds for the pit wall. The dash is its own app with a second origin, NATS ([0001](0001-v2-architecture.md), point 6). |
| [0004, design language](https://github.com/those-who-run/those-who-run/blob/main/docs/adr/0004-design-language.md) | Holds for `pitwall` and `dash` as written. |
