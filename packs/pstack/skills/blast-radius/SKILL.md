---
name: blast-radius
description: Find what a change could break somewhere else, beyond its diff, and prove the fact its safety rests on by running real code. For a role that checks a change and edits nothing.
---

# Blast radius

Find what a change breaks somewhere else, before it ships. Listing the callers is not the job. Anyone can grep those in a second. The job is the breakage that grep does not show.

## Don't trust your own writeup

A blast-radius writeup that sounds right is worthless. It reads as convincing whether or not it is true. So don't hand back a writeup. Find the one or two facts the whole thing depends on, and prove them by running code.

For each fact the change's safety depends on, get it as far down this list as is cheap, and say where it stopped:

1. You said so. Worthless by itself.
2. You pointed at the line. A real `file:line`, or the library's own source.
3. You showed the bad case can't happen. You walked the failure step by step and it does not reach.
4. You ran it. A script or a test that calls the real code and fails loudly if you are wrong.
5. You reproduced it in the running program.

Step 4 is usually one small script that imports the same library the program ships and calls the exact function you are worried about.

## Steps

1. **Read the change.** The diff, the symbols it adds, changes and deletes, and what it now does differently, the part the diff does not spell out included. Read the commits and what the change says it is for.
2. **Check the claim it makes about itself.** A fix says the bug is gone: run the reproduction on the code before the fix and after. A refactoring says behavior is unchanged: run the pin. A change that fails its own claim needs nothing more from you.
3. **Find the one fact it is safe because of.** Most changes that look risky are safe because of a single fact, such as "this call only drops cache entries that are already dead, and does nothing else". Find that fact. If it holds, most risky cases clear at once. Spend your time here, not on a long list of maybes.
4. **Look where grep stops.** Read the source of the library the change calls, at the version the project pins, with any local patch. Work out when things run: startup, teardown, callbacks, retries. Follow what a symbol search misses: the JSON an API returns, a database column, a wire format, another program reading the same bytes, a feature flag, code three hops downstream, a name in a string.
5. **Be honest about each risk.** Give it a real chance of happening and a real cost if it does. Keep the risks you confirmed. List the ones you checked and cleared apart from them. Cite a real `file:line`. A search that finds nothing is still an answer. Never make up a caller or an interface.
6. **Prove the one fact.** Write a script or a test that runs the real code, run it, and paste what happened.

Scripts go in a temporary directory, not in the files under review. To run the code as it was at another commit, check that commit out in a temporary directory (`git worktree add`), not here.

## What to hand back

- **What it does.** What changed, the part that is not obvious included.
- **Its own claim.** Whether it holds, with the command and the output.
- **The one fact it is safe because of.** State it, say which step of the list you got it to, and show the proof. If you could not prove it, write "unproven".
- **Risks.** Each names how it breaks, the `file:line`, how likely and how bad, and how to check. Paste the proof for the ones that matter.
- **Cleared.** What you checked, and why it is fine.
- **Before it merges.** The cheapest test or reproduction that catches the real bug, the script you wrote included.

A proven break, or a change that fails its own claim, means the change is not ready. A risk you could not prove is a risk, not a verdict: say so, and let the reader weigh it.
