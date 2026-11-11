---
name: refactor
description: Change structure without changing behavior. Pin today's behavior, name the target shape, subtract first, then move in small steps that keep the pin green. For the roles of a refactoring.
---

# Refactor

The structure changes. The behavior does not. That sets this apart from a feature, which adds behavior, and a bug fix, which corrects it.

If the cleanup turns up a missing feature or a real bug, write it down and leave it. The structural change ships first, against the pinned behavior. A redesign is allowed, but name it as one.

Your message names the part to do. The principles named below are files in `../principles/`. Read the file of each one you apply.

## Pin

Capture today's behavior before any structure moves.

1. Learn the contract. Read the code, its callers and its tests. What do its users observe: return values, errors, state they can read back, effects on other systems?
2. Write the pin: a characterization test, a snapshot, or an equivalence harness (a script that runs old and new and diffs their output). It captures what the code does today, quirks included. A type check or a lint is not a pin. Where the area has no coverage, the pin comes first.
3. Run it. It is green on today's code. Commit it alone.

Write the pin file: what is pinned and what is not, the command that runs the pin, and its output now. Move no structure in this part.

## Shape

Say where the code is going before anyone moves it.

1. Name the structure the code is missing (`../principles/model-the-domain.md`). Boring code stays when its shape is already clear and local. The reshape must delete branches or invalid states, not add indirection.
2. Name the target: the module layout, the types and the call graph as they would be if built today (`../principles/foundational-thinking.md`, `../principles/redesign-from-first-principles.md`).
3. Name what goes: dead code, wrappers with one caller, redundant validators, references to nothing.
4. Order the moves: the subtraction first, then each step that keeps behavior.
5. Say what the change buys. The measure is reader load: the layers a reader traces and the state they hold in their head (`../principles/minimize-reader-load.md`). If the target does not lower it somewhere, say so, and say that the refactoring is not worth doing.

Write the shape file in these sections: Today, Missing structure, Target, What goes, Steps, Reader load. Move no structure in this part. Whoever reshapes reads only this file and the pin.

## Reshape

Move toward the shape file in small steps, the pin green after each.

- **Subtract first.** Delete what the shape file says goes, before the new shape arrives (`../principles/subtract-before-you-add.md`). The smallest change that reaches the target ships (`../principles/laziness-protocol.md`). A cleanup that "might help" gets reverted.
- **One step, one check.** Make a step, run the pin, commit. Then the next (`../principles/sequence-verifiable-units.md`).
- **One wave for an interface.** Move every caller and delete the old interface in the same step. No shim, no old path beside the new (`../principles/migrate-callers-then-delete-legacy-apis.md`).
- **Check every rename against the files.** A rename silently misses usages in strings, prose, config and comments. Search for the old name when you are done.
- **Mechanical edits get a tool.** For many edits of one kind, write the script or codemod that does them, and rerun it instead of editing by hand (`../principles/build-the-lever.md`).
- **The pin's tests stay as they are.** If the pin has to change, the behavior changed. Stop and ask.
- **Prove it on the real artifact**, not by "it compiles" (`../principles/prove-it-works.md`). For a larger reshape, diff old output against new, or replay a recorded baseline.
- **Keep only what earned it.** If a step does not lower reader load, revert it.

Commit in order: the subtraction, the reshape, then any cleanup that follows.

## Your reply

The structure that changed, the pin you held it against, the proof that behavior is unchanged (the command and its output), where reader load went down, and what you reverted. No new behavior.
