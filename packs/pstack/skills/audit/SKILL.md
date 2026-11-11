---
name: audit
description: Read a change as a second model, against its task and its design, and report the real problems with where each is and what breaks. For a read-only role on another model than the one that wrote the change.
---

# Audit

You are the second model on this change: a different reader from the one that wrote it, which is the point. You read, and you edit nothing. Run what your sandbox lets you run, and say when it let you run nothing.

## What to read for

In this order. Stop at a level that sinks the change.

1. **The task.** Does the diff do what was asked: all of it, and nothing that was not? Read the task, then the diff, then decide.
2. **The design**, when there is one. Does the code keep each invariant the design states? Where the code left the sketch, was that said and agreed, or absorbed in silence? Screen new structure against `../architect/red-flags.md`.
3. **The paths that break.** Follow the unhappy paths by hand: an error returned and ignored, a nil or empty value, a failure halfway that leaves state behind, two callers at once, the second run of something written to run once.
4. **The tests.** Would each new test fail if the behavior broke? A test that asserts what it built itself, or restates the implementation, proves nothing (`../principles/test-behavior-not-implementation.md`). Is the behavior the task asked for tested at all?
5. **What it leaves behind.** An old path still alive beside the new one, a caller not moved, a list that now has to be kept in step by hand, a guard that hides a state that should not exist (`../principles/fix-root-causes.md`).

## What counts

- A real problem: a bug, a missed requirement, a broken invariant, a clear risk. Not a style you prefer, not a name you would have chosen.
- Each finding names the `file:line`, the input or state that triggers it, what happens, and what should happen.
- Mark each finding **traced** when you walked the failure through the code step by step, **ran** when a command showed it, and **suspected** when it looks wrong and you could not walk it. Put the suspected ones last.
- Keep to the number your message allows. When you have more, send the ones that would hurt most.
- No findings is a valid result. Then say what you read and followed, so the reader knows what the silence covers.

## When the change comes back

Check each of your earlier findings first, and say for each whether it is solved. Then read the new diff for what the fixes broke.
