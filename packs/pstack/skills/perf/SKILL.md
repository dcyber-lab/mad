---
name: perf
description: Make something faster against a measurement. Capture a baseline you can trust, try one hypothesis at a time, keep only what the number proves, and have the number measured again by someone else. For the roles of a performance fix.
---

# Perf

Tie every change to a measurement. Do not read source instead of measuring, and do not claim a ceiling you have not run.

Your message names the part to do. The principles named below are files in `../principles/`. Read the file of each one you apply, `../principles/explain-the-number.md` first.

## Baseline

1. Write the claim you expect to make, in the words you would ship: "export is 30% faster at p50 on the 60k-row dataset". Everything below tests that sentence.
2. Build the harness: one command that runs a realistic workload and prints the number, a count of the work done, and a count of errors. Know what it times, what it counts, and what it ignores. If the complaint does not reproduce in the harness, fix the harness first.
3. Look at the machine: the load (`uptime`) and the cores. If it is busy and you cannot stop that, alternate the sides you compare, and say so.
4. Run it at least five times. Record the median and the range. A gap smaller than the spread between runs is no difference.
5. Find the limiter. Ask why the number is not twice as good, and answer from a profile or from system counters taken in a run you do not report, mapped to source. A guess from reading the code is not a limiter.

Write the perf file: the claim, the command, the runs with median and range, the limiter and how you found it, and what the harness ignores. Change no behavior in this part.

## Improve

One hypothesis at a time. Each names a mechanism ("defer X off the boot path, because it blocks first paint"), never "try memoizing something". Try these in order, the cheapest first, and stop when one meets the target:

1. Don't do it. Stop work whose result nothing uses.
2. Do it, but don't do it again.
3. Do it less.
4. Do it later.
5. Do it when they're not looking.
6. Do it concurrently.
7. Do it cheaper.

For each attempt:

- Measure before and after with the same harness, alternating the two. Run the tests.
- Keep the change only when the number moves past the noise and the tests stay green. Otherwise revert it whole. A tweak that "might help" is not kept.
- One commit per kept change.
- Add a line to the perf file either way: the hypothesis, the change, before, after, kept or reverted.

Never stack changes you have not measured apart. Correctness and simplicity outrank the number: revert a win that breaks behavior, and keep a simplification that holds the number.

## Check

You measure again, apart from whoever made the change. Run the harness yourself on the code as it was and as it is, alternating (A, B, A, B), at least five runs a side. To run the code as it was, check that commit out in a temporary directory (`git worktree add`), not here.

Then answer each question with evidence from a run, not from the code:

1. **Why not double?** Name the limiter now. If a change did not move the number, the limiter says why.
2. **Was it tuned?** Both sides ran the way production runs them: release builds, the same flags, the same data, caches as warm or cold as production has them. A side on defaults means you compared configurations.
3. **Did it break limits?** Do the arithmetic. Removing a piece that takes 10% of the run makes the run at most about 11% faster. A result past a limit measured something else: a cache, a no-op, a bug.
4. **Did it error?** Count failures, and check that the outputs are correct, not only present. Rejections are fast.
5. **Does it reproduce?** Median and range over your own runs. A gap inside the spread is no measurable difference.
6. **Does it matter?** Next to a micro result, measure the path a user waits on, and give the micro result as a share of it.
7. **Did it even happen?** The work ran inside the timed region: the request arrived, the rows were written, the result was used.

## Your reply

Lead with the verdict: faster, slower, no measurable difference, or inconclusive. Give the number with its unit, the run count, the range and the limiter: "p50 41 ms to 33 ms, median of 7 runs a side, range 32 to 35 ms after, bound by JSON parsing on one core". The verdict is inconclusive when you claim a difference and cannot name the limiter, when a side ran untuned, or when questions 4 and 7 went unchecked. Name the gap.
