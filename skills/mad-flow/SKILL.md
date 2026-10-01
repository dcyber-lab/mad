---
name: mad-flow
description: Design a flow for mad runs with the user (which agents play which roles, with what model and effort, and the steps, reviews, questions and gates a task goes through) and save it as a JSON file mad checks and runs. Use when the user wants to create, change or explain a mad flow or run process, e.g. "opus designs, codex implements, two reviewers", or asks how mad hands a task between agents.
---

# Designing mad flows

A **run** in mad hands one task through agents in roles: each agent is a
real claude or codex TUI in the deck, and they share one git worktree. A
**flow** is a run's template, a JSON file. You design flows with the user
and write the file; mad checks it and runs it.

## How to work

1. **Find out what the user wants.** Ask only what changes the flow and
   they have not said:
   - the roles, and who plays each: agent, model, effort;
   - the steps in order; where a review sends work back, and how often;
   - where the run should stop for them (gates), and who may ask whom.

   Where they leave something open, take the defaults below and say so.
2. **See what is there.** `mad run flow ls` lists the flows this project
   can use; `mad run flow show design-impl-review` prints the default
   one, a good start. `mad run flow agents` lists the agents this machine
   can run, with their efforts.
3. **Write the file**: `.mad/flows/<name>.json` at the repository's root
   for the project (and the team: it can be committed), or
   `~/.config/mad/flows/<name>.json` for the user alone. A file named as
   an existing flow takes its place.
4. **Check it**: `mad run flow check .mad/flows/<name>.json`, and fix what
   it reports until it says the flow is good. Never hand over a flow that
   has not passed.
5. **Show it** in a few lines, the agents included:

   ```
   design (designer: claude opus@high)
     → implement (builder: claude sonnet; may ask the designer)
     → review (reviewer: codex, read only): CHANGES → implement, at most 3 rounds
   gate: after design
   ```
6. **Offer to try it**: `o` on the project in mad's sidebar, then pick the
   flow in the form; or `mad run start -f <name> "<task>"`.

The user may run mad under another name (`mad-pre` for a preview build):
use the one they use.

## The format

```json
{
  "name": "design-impl-review",
  "description": "design → implement → review",
  "roles": [
    {"name": "designer", "agent": "claude:opus"},
    {"name": "builder", "label": "implementer", "agent": "claude:sonnet"},
    {"name": "reviewer", "agent": "codex", "read_only": true}
  ],
  "steps": [
    {"name": "design", "role": "designer", "gate": true,
     "prompt": ["Task: {{task}}", "", "Read the relevant code, then write a design to {{run}}/design.md; change no code."]},
    {"name": "implement", "label": "implementation", "role": "builder", "ask": ["designer"],
     "prompt": ["Task: {{task}}", "", "Implement {{run}}/design.md, run the relevant tests, and commit."],
     "again": ["Review findings (round {{round}}):", "{{review}}", "", "Fix them, run the tests, and commit."]},
    {"name": "review", "role": "reviewer", "review": true, "back": "implement", "max_rounds": 3,
     "prompt": ["Task: {{task}}", "", "Review git diff {{base}} against {{run}}/design.md. Report only real problems, at most 5."]}
  ]
}
```

| Field | What it is |
|---|---|
| `name` | lowercase letters, digits, dashes |
| `description` | one line, shown in the form |
| `roles[].name` | how steps refer to the role |
| `roles[].label` | what the sidebar calls it (default: `name`) |
| `roles[].agent` | `claude:opus`, `claude:sonnet`, `claude:haiku`, `codex` (the model its config sets), `codex:<model>`; add `@effort`: `claude:opus@high`, `codex:gpt-6-sol@xhigh` |
| `roles[].read_only` | looks and changes nothing: it can only review |
| `steps[].name`, `label`, `role` | the step, what to call it, who does it |
| `steps[].prompt` | what the step is to do: a string, or a list of lines |
| `steps[].again` | the prompt when the step runs again after a review asked for changes (mad has a default) |
| `steps[].review` | its reply ends in a verdict |
| `steps[].back` | for a review: the earlier step its changes go back to |
| `steps[].max_rounds` | for a review: how often it may send work back (default 3; then the run waits for the user) |
| `steps[].ask` | roles the step may put a question to; the answer comes back to it |
| `steps[].gate` | the run may stop after this step for the user (they tick "gate" when they start it) |

Placeholders in prompts: `{{task}}` the task, `{{run}}` the run's
directory (relative to the worktree), `{{base}}` the commit the run
started from, `{{review}}` what the last review found and `{{round}}` its
round (both for `again`).

## What mad does itself: leave it out of prompts

- **How to end a reply.** mad adds to every message: "make the last line
  just DONE", or for a review "VERDICT: APPROVE or VERDICT: CHANGES",
  and for a step with `ask` how to ask (`QUESTION(@role): ...`).
- A header naming the run, the step and the role, and that the work is
  handed on through the files in `{{run}}/`.
- Sending a review's findings back, waiting for the user (permission
  prompts, gates, budget, the five-hour window), notifications, cost, and
  compacting a role whose context passed 150k tokens.

## What a flow cannot do (yet)

- **Steps run one at a time, in order.** No two at once: not two
  implementers side by side, not two reviewers in parallel. Two reviews
  one after the other work.
- **The only branch is a review's verdict**: APPROVE goes on, CHANGES goes
  back to `back`, and every step from there runs again.
- **Each role is one agent session for the whole run.** A role in several
  steps keeps what it learned (and its prompt cache). Use another role
  for fresh eyes.
- Only claude and codex play roles. A codex `read_only` role works in
  codex's read-only sandbox; a claude one loses its edit tools.
- No per-step budgets or timeouts: those are the run's.

When the user wants one of these, say so plainly, and offer the closest
flow that works.

## Writing good prompts

- One job per step, in a few sentences: what to read, what to make, and
  where to put it.
- Hand over through files in `{{run}}/` (design.md, test-plan.md, notes):
  say which file a step writes and which the next one reads. Reviews read
  `git diff {{base}}` (uncommitted work included) and those files.
- A step that changes code runs the relevant tests and commits. A review
  reports only real problems (bugs, missed requirements, clear risks), a
  handful at most, each naming the file.
- Start with `Task: {{task}}` where the step needs the task.
- Write prompts in English unless the user wants another language; agents
  answer in the language the task is written in.
- Keep them short: each costs its tokens every time a step runs.

## Defaults

When the user does not say: design on `claude:opus`, implementation on
`claude:sonnet`, review on `codex`, read only; three rounds of review; a
gate after the design; the implementer may ask the designer.

## Another example: tests first, two reviewers

```json
{
  "name": "tests-first",
  "description": "tests → implement → review → second review",
  "roles": [
    {"name": "tester", "agent": "claude:opus@high"},
    {"name": "builder", "label": "implementer", "agent": "codex:gpt-6-sol"},
    {"name": "reviewer", "agent": "claude:sonnet", "read_only": true},
    {"name": "auditor", "agent": "codex", "read_only": true}
  ],
  "steps": [
    {"name": "tests", "role": "tester", "gate": true,
     "prompt": ["Task: {{task}}", "", "Write tests that pin the task down, and that fail now. List them in {{run}}/tests.md. Change no other code."]},
    {"name": "implement", "role": "builder", "ask": ["tester"],
     "prompt": ["Task: {{task}}", "", "Make the tests in {{run}}/tests.md pass without changing them. Run all tests, and commit."]},
    {"name": "review", "role": "reviewer", "review": true, "back": "implement",
     "prompt": ["Task: {{task}}", "", "Review git diff {{base}}: is the task done, and are the tests sound? Report only real problems, at most 5."]},
    {"name": "audit", "role": "auditor", "review": true, "back": "implement", "max_rounds": 2,
     "prompt": ["Task: {{task}}", "", "Look at git diff {{base}} for security and data risks only. Report only real problems, at most 3."]}
  ]
}
```
