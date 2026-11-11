---
name: bugfix
description: Fix a defect the scientific way. Reproduce it, find the cause with evidence from a run, have the diagnosis challenged, then make the smallest fix the evidence justifies. For the roles of a bug fix.
---

# Bug fix

Be scientific. Every line that ships traces to something that ran. A change that "might help" is a hypothesis, not a fix, and it does not ship. When evidence refutes a hypothesis, undo what it motivated.

Your message names the part to do. The principles named below are files in `../principles/`. Read the file of each one you apply.

## Reproduce

Make the bug happen yourself, on the surface where it was reported, before you read for a cause.

1. Say the bug back in your own words: what should happen, what happens instead, and for whom.
2. Reproduce it. If it will not fire directly, synthesize the trigger, tighten the conditions, or add instrumentation until it does. Do not hand the reproducing back to the person.
3. Make the reproduction one command that anyone can rerun:
   - When the bug has a cheap local test path, write the smallest test that fails because of the bug, where the code's tests already live and in their style. The test states the intended behavior, not the current implementation. Run it. It must fail, and for the right reason. Commit it alone.
   - When a test would need broad setup, brittle mocks or slow infrastructure, write a script that exits non-zero while the bug is there. A bad test is worse than none.
4. Write the bug file: the command, what it printed, and what the intended behavior would print.

Fix nothing in this part. If the bug does not reproduce after real effort, say what you tried and what it would take. That is a result too.

## Diagnose

Find the mechanism, and show it with evidence from a run.

1. List the candidate causes. Read the code path, and the history of the lines involved (`git log -p`, `git blame`). A regression has a commit.
2. Rule them out by halves. Each pass, take the split that cuts the most of what is left, get evidence from a run, and eliminate. When state is unclear, add logging or a probe and read it as the code runs. Do not guess.
3. Ask why at each step until the answer is a wrong decision in the code, not a consequence of an earlier one (`../principles/fix-root-causes.md`). The symptom is where a wrong state became visible. The root cause is the first place the system did something it should not do, or failed to handle a state it should handle.
4. Ask whether the state that triggered the symptom is legitimate. If the system may be in that state, the code that broke on it is the cause. If the state should never exist, find what produced it.
5. If two explanations in a row failed the same check, write down what both assumed, and test that (`../principles/attack-the-premise.md`).

Take your probes out again. This part leaves the code as it found it. Add to the bug file: the root cause with its `file:line`, the evidence that shows it (the command and its output), the causes you ruled out and how, and the other places with the same fault.

## Challenge

You are a second reader of the diagnosis, on another model. Find where it rests on a guess.

Read the bug file, then the code it cites. For each link in its chain from trigger to symptom, ask whether something that ran showed it, or it only sounds right.

- Does the cited evidence exist, and does it say what the file claims? Open every `file:line`.
- Does the cause explain all of the symptom, and the cases where the bug does not happen?
- Is it the first wrong decision, or a consequence of an earlier one?
- Does another cause fit the same evidence? Name the observation that would tell the two apart.
- Would a fix at the named place make the reproduction pass? Walk it through.

Report only doubts that matter to the fix, at most five. Give each its `file:line`, what is unproven, and the run that would settle it. Approve a diagnosis that holds, without a list.

## Fix

Fix the cause the bug file names, with the smallest change the evidence justifies.

- Fix the fault, not the place it surfaced. A guard, a retry, a swallowed error or a clamped value at the place the bad state showed hides the bug.
- If the fix crosses a function boundary, name the shape first: the data, the signature that changes, who calls it.
- Fix every place with the same fault, and say which.
- Run the reproduction. It passes now. Run the tests around it. Add the output after the fix to the bug file, under the output from before.
- Commit the fix after the failing test, so that history shows red, then green.
- If a workaround needs a paragraph of comment to justify it, the code is wrong.

Leave no change the evidence does not need. If the deciding fact took a deep dig to find, say in your reply which cheap, always-on signal would have shown it at first look.

## Your reply

What was broken, the root cause, the fix, and how you verified it. Give the failing output and the passing output as printed.
