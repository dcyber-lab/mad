# Rationale template

The prose that goes with a sketch. One page. Sentence-case headings, no boilerplate. Replace each note in italics with the content.

## Problem

*One paragraph. What we are trying to do, and what about the existing system or its constraints makes the shape non-obvious. Name the constraints that grounding found (types to interoperate with, callers that cannot break, invariants that cross the boundary), so the reader sees what you saw.*

## Usage (the caller's view)

*Write this first, before the sketch. Show what the consumer reads, and two or three realistic call sites in their own code: what they import, what they call, what comes back. The sketch in Shape is derived from this, and the two must agree. When they diverge, change the sketch, not the usage.*

## Shape

*The recommended architecture. Data structures first, then how data flows through the signatures. Name the decisions the rest depends on. State which invariants are encoded in types, where validation lives, and what the system deliberately does not do. Judge interface depth in so many words: what complexity the public surface hides, what stays exposed to callers, and why the interface is no larger than needed. Name the principle behind each decision without restating it.*

## Decision

*Filled in when candidates are chosen between. Which candidate became the base and why, what was taken from each of the others, and what was rejected and why. For a single candidate, leave it out.*

## Tradeoffs accepted

*One bullet per tradeoff the shape makes, in the form "we accept X in exchange for Y". Name anything a later reader might take for an oversight.*

## Alternatives considered

*Required. At least one concrete alternative shape, with a line on why it lost. Judge each on interface depth, not on implementation simplicity alone: what it exposes to callers and what it hides. One is enough when the constraints forced the answer. Say then why this was the only shape that works. Do not list flavors of one shape.*

## Open questions and risks

*What the person needs to weigh in on, and risks worth knowing before the build starts. Write them as questions, so that the answer settles them.*

## Next step

*The first thing to build against the sketch. One sentence.*
