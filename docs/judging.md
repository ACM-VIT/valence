# Judging

How a submission moves from "submitted" to a verdict.

## Submissions and attempts

A **submission** is what a user sent: problem, language and source. It
never changes. An **attempt** is one judging of it. Attempt 1 is created
on submit; a rejudge adds attempt 2 and leaves attempt 1's results in
place. The `submissions` row mirrors its current attempt's status and
verdict, so listing submissions never needs a join.

`test_results` has one row per test per attempt. Judging stops at the
first failing test, so a WA on test 3 has three rows.

## States

```
queued ──▶ leased ──▶ compiling ──▶ running ⇄ checking
              ▲            │           │         │
              │            └───────────┴────┬────┘
              │                             ▼
       retryable_error ◀── (infra error)  finalized
```

| Status | Meaning |
|---|---|
| `queued` | Waiting for a worker. |
| `leased` | A worker took the job and holds its lease. |
| `compiling` | Building the source. |
| `running` | Running test `current_test`. |
| `checking` | Comparing test `current_test`'s output with the answer. |
| `retryable_error` | Infrastructure failed (not the user's code); the job goes back to the queue. |
| `finalized` | Done. `verdict` is set, and nothing changes after this. |

Allowed changes, enforced by `CanTransition` in
`services/api/internal/submissions/status.go`:

- The forward path: `leased → compiling → running → checking`, and `checking → running` for the next test.
- `→ leased` from any unfinished state. A worker that crashes loses its lease, and the next worker restarts the attempt from wherever it stopped.
- `→ retryable_error` or `→ finalized` from any state except `queued`.
- Nothing leaves `finalized`.

Every change also names the status it expects to move *from* and carries
the worker's queue lease token. Moving to `leased` records the token on
the attempt; every later change, including finalizing, must carry the same
token. The expected status alone isn't enough: a worker whose lease expired
can wake up when its replacement has reached that same status again. The
token check refuses it, so only the current lease holder writes to an
attempt. A `queued` attempt has no token, so it can't be finalized.

## Verdicts

| Verdict | Meaning |
|---|---|
| `AC` | Accepted: every test passed. |
| `WA` | Wrong answer. |
| `TLE` | Time limit exceeded. |
| `MLE` | Memory limit exceeded. |
| `RE` | Runtime error: crashed or exited non-zero. |
| `OLE` | Output limit exceeded. |
| `CE` | Compile error; `compile_output` says why. |
| `IE` | Internal error: judging failed for reasons that aren't the user's fault, after retries. |

`time_ms` and `memory_kib` on the submission are the largest over its tests.

## Reporting results twice

A worker whose connection drops will send its final report again.
Finalizing only changes an attempt that isn't finalized yet, and test
results are inserted with `ON CONFLICT DO NOTHING`, so the second report
changes nothing and the first result stands.