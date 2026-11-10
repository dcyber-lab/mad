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
   one, a good start; `mad run flow show pstack-bugfix` one whose roles
   work by a pack's skills and whose steps are checked. `mad run flow
   agents` lists the agents this machine can run, with their efforts.
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
| `roles[].no_edits` | runs commands (tests, a repro in a temporary directory) but edits no file in the worktree outside `{{run}}/` and makes no commit: a reviewer that proves what it finds. mad refuses its edits there and checks the worktree after its steps. Runs only; a codex gets its read-only sandbox |
| `roles[].skill` | a skill the role works by. `pstack:bugfix` is the skill `bugfix` of the pack `pstack` (`mad pack show pstack` lists them): the run keeps a copy, and any agent can play the role. A name no pack has is one the role's agent has itself (`plugin:name` for a claude plugin's, else the skill's name). The run does not start when the skill is nowhere |
| `steps[].name`, `label`, `role` | the step, what to call it, who does it |
| `steps[].prompt` | what the step is to do: a string, or a list of lines |
| `steps[].again` | the prompt when the step runs again after a review asked for changes (mad has a default) |
| `steps[].review` | its reply ends in a verdict |
| `steps[].back` | for a review: the earlier step its changes go back to |
| `steps[].max_rounds` | for a review: how often it may send work back (default 3; then the run waits for the user) |
| `steps[].ask` | roles the step may put a question to; the answer comes back to it |
| `steps[].gate` | the run may stop after this step for the user (they tick "gate" when they start it); after a review, when it lets the work through |
| `steps[].expect` | what the step must leave behind the first time it runs: `files` (exist, not empty; `{{run}}/bug.md`), `commit` (at least one new commit), `tests_first` (a commit of tests alone before the first commit of anything else). mad tells the role, checks when it ends, sends what is missing back once, then waits for the user |
| `steps[].expect.unchanged` | the step leaves the worktree as it found it, whatever it did meanwhile: for a step that reads, measures or designs, whose work goes in `{{run}}/`. Checked every time the step runs. Not with `commit`, and not for a `read_only` or `no_edits` role, which changes nothing as it is |

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
- What it checks and refuses. A role that pushes or opens or merges a
  pull request is refused: the user does that when the run is done. A
  step asks only the roles in its `ask`; a question to anyone else is
  turned back ("decide, say what you assumed"). A step's `expect` and a
  `no_edits` role's untouched worktree are checked when the step ends.
- For a role with a `skill`: where the skill's SKILL.md is, to read it
  in full before the role's first step and follow it, and that the run
  wins where they differ (the worktree alone, no push or pull request,
  how to end). A prompt names the skill's part to do ("Do the skill's
  Diagnose part"), not its path.
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
- Every role works in the run's worktree alone: mad refuses its edits
  elsewhere. A step can't write to the main checkout or another repo.
- No per-step budgets or timeouts: those are the run's.

When the user wants one of these, say so plainly, and offer the closest
flow that works.

## Several flows that belong together: a pack

When the user wants a way of working, not one flow (flows and team
templates that share skills, and the same agents in the same kinds of
role), make it a pack: a directory in `.mad/packs/<name>/` (the
project's) or `~/.config/mad/packs/<name>/` (the user's).

```
pack.json         {"name": "house", "version": "1", "description": "…",
                   "agents": {"strong": "claude:opus@high", "other": "codex"}}
flows/*.json      flows, as above; a role's agent may be "$strong"
teams/*.json      team templates (mad team check)
skills/<name>/    SKILL.md: what a role works by, named "house:<name>"
```

- `mad pack ls` lists the packs here, `mad pack show pstack` what one
  has. `mad pack eject pstack` copies a built-in pack to change it; the
  copy takes its place.
- `"agents"` says once who plays what across the pack; a role refers to
  one as `"$strong"`.
- A skill is written for the role that reads it: what to do in each part
  of its work, and what to hand back. It names files beside it by path
  (`./checklist.md`, `../principles/prove-it-works.md`).
- `mad pack check` until it says the pack is good: every flow and
  template reads, every skill a role names is there, and no skill points
  at a file that is not.

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
