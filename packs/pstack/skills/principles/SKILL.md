---
name: principles
description: The engineering principles the pack's skills build on, one file each, with when each applies. Read the file of a principle before you apply it.
---

# Principles

Each line says when a principle applies. Its file is beside this one, named in backticks. Read the file in full for any principle you apply, and in your reply name each principle that shaped a decision and the choice it changed. Name only principles whose file you read.

## Core

- **Laziness Protocol** (`./laziness-protocol.md`). Refactoring, sizing a diff, or tempted to add abstractions, layers, or signal threading. Bias to deletion and the smallest change that solves the problem.
- **Foundational Thinking** (`./foundational-thinking.md`). Before writing logic: core types and data structures, scaffold before feature, what concurrent actors share.
- **Redesign from First Principles** (`./redesign-from-first-principles.md`). Integrating a new requirement into an existing design. Redesign as if it had been there from day one.
- **Attack the Premise** (`./attack-the-premise.md`). Two or more fixes that share one premise have failed the same check. Write the premise down and test it before another fix that assumes it.
- **Subtract Before You Add** (`./subtract-before-you-add.md`). Sequencing an addition, refactor, or rewrite. Remove dead weight first, then build on the simpler base.
- **Minimize Reader Load** (`./minimize-reader-load.md`). Reviewing or shaping code that is hard to trace. Count layers and hidden state, collapse one-caller wrappers, shrink mutable scope.
- **Outcome-Oriented Execution** (`./outcome-oriented-execution.md`). Planned rewrites and migrations with explicit phase boundaries. Converge on the target, don't preserve throwaway compatibility states.
- **Experience First** (`./experience-first.md`). Product, UX, or feature-scope tradeoffs. Choose the user's delight over implementation convenience.
- **Exhaust the Design Space** (`./exhaust-the-design-space.md`). A novel interaction or architectural decision with no precedent. Build two or three competing prototypes and compare before committing.
- **Build the Lever** (`./build-the-lever.md`). Any non-trivial work. Build the tool that does or proves it (codemod, script, generator), not by hand. The tool is what a reviewer reruns.

## Architecture

- **Model the Domain** (`./model-the-domain.md`). Writing stateful logic, or code that branches a lot or repeats a shape assumption across files. Encode the domain in a structure instead of scattered conditionals.
- **Boundary Discipline** (`./boundary-discipline.md`). Wiring validation, error handling, or framework adapters. Guards at system boundaries, trust internal types, keep business logic pure.
- **Type System Discipline** (`./type-system-discipline.md`). Designing types or a signature in a typed language. Make illegal states unrepresentable, brand primitives, parse external data at boundaries.
- **Make Operations Idempotent** (`./make-operations-idempotent.md`). Commands, lifecycle steps, or loops that run amid crashes and retries. Converge to the same end state.
- **Migrate Callers Then Delete Legacy APIs** (`./migrate-callers-then-delete-legacy-apis.md`). A new internal API while old callers exist. Migrate and delete in one wave.
- **Separate Before Serializing Shared State** (`./separate-before-serializing-shared-state.md`). Concurrent actors might write the same file, branch, key, or object. Remove the sharing first.

## Verification

- **Prove It Works** (`./prove-it-works.md`). After a task, before declaring done. Verify against the real artifact, not a proxy or "it compiles".
- **Fix Root Causes** (`./fix-root-causes.md`). Debugging. Trace each symptom back to the first place the system went wrong and fix it there.
- **Sequence Work into Verifiable Units** (`./sequence-verifiable-units.md`). Multi-step work, and how you stack commits. Small units that each end in a check, verified before the next.
- **Test Behavior, Not Implementation** (`./test-behavior-not-implementation.md`). Writing, changing, or keeping a test. Assert what the code's users can observe.
- **Explain the Number** (`./explain-the-number.md`). Before you measure, and before you trust, report, or act on a number you measured. Find what limits it, and rule out that it measured something else.

## Working with others

- **Guard the Context Window** (`./guard-the-context-window.md`). Context fills up: large outputs, long files, repeated reads. Keep summaries, not raw payloads.
- **Never Block on the Human** (`./never-block-on-the-human.md`). Tempted to ask "should I do X?" on reversible work. Proceed, present the result, let the person correct course.
- **Encode Lessons in Structure** (`./encode-lessons-in-structure.md`). You catch yourself writing the same instruction a second time. Encode it as a lint, flag, runtime check, or script instead of more text.
