# pstack, as a mad pack

[pstack](https://github.com/cursor/plugins/tree/main/pstack) is Lauren
Tan's skill pack for rigorous engineering: ground before you design,
design it twice, prove what you claim by running it, and put a second
model where a second opinion counts. There, one chat runs a playbook and
starts subagents of its own.

This pack is the same way of working with mad as the one that runs it.
Each phase of a playbook is a step of a flow, each second opinion is a
role with its own agent, and what a phase must leave behind is checked by
the runner instead of asked for in a prompt. It needs no plugin: the
skills its roles work by are the files in `skills/`.

## What is in it

| Flow | Steps |
|---|---|
| `pstack-feature` | ground → sketch (claude) → second sketch (codex) → design, the sketch committed → build → interrogate (proves by running, edits nothing) → audit (codex, read only) |
| `pstack-bugfix` | reproduce → diagnose (leaves the code as it found it) → challenge (codex reads the diagnosis) → fix → blast radius |
| `pstack-refactor` | pin today's behavior → shape → reshape → blast radius |
| `pstack-perf` | baseline → improve, one hypothesis at a time → second measurement |
| `pstack-docs` | write → fact check (codex) → edit |

| Team | Roles |
|---|---|
| `pstack` | architect (lead, picks the playbook), builder, interrogator (edits nothing) |
| `pstack-research` | investigator (lead), checker (codex, read only) |
| `pstack-docs` | writer (lead), fact-checker (codex), editor |

`pack.json` names three agents, and every role is played by one of
them: `$strong` for judgment (design, diagnosis, adversarial review),
`$fast` for writing the code, `$other` for the second model. Change who
plays what there, once.

## Changing it

The pack is files, and mad reads them when a run or a team starts. To
change it without building mad:

```sh
mad pack eject pstack      # a copy in ~/.config/mad/packs/pstack/ (-p: in the project)
$EDITOR ~/.config/mad/packs/pstack/flows/pstack-bugfix.json
mad pack check             # the flows and templates read, the skills they name are there
```

The copy takes the built-in pack's place whole. A run keeps the flow and
the skills it started with, in `.mad/runs/<name>/`, so a change shows in
the next run and never in one under way; `run.json` there says which
pack and version it followed. To work on the pack in a checkout of mad,
link it instead of copying: `ln -s $PWD/packs/pstack ~/.config/mad/packs/pstack`.

A skill is `skills/<name>/SKILL.md`, and a role names it
`pstack:<name>`. Skills point at each other by path (`../principles/fix-root-causes.md`),
and `mad pack check` fails on a path that is not there.

## Where the text comes from

Adapted from pstack 0.15.36 (MIT, see `LICENSE`). What changed is what
mad now does itself: fanning work out to other models, choosing them,
opening pull requests.

| Here | From pstack |
|---|---|
| `skills/principles/*.md` | the `principle-*` skills, their text kept |
| `skills/unslop/`, `skills/technical-writing/` | the skills of those names, their text kept |
| `skills/architect/` | `architect`, with `arena`'s pick and graft as its Choose part; `red-flags.md` kept |
| `skills/build/` | the Feature playbook's build steps, `poteto-mode`'s rules for code and comments |
| `skills/bugfix/` | the Bug fix playbook, `tdd`, `principle-fix-root-causes` |
| `skills/refactor/` | the Refactoring playbook |
| `skills/perf/` | the Perf issue and Hillclimb playbooks, `benchmark-checklist` |
| `skills/interrogate/`, `skills/blast-radius/` | the skills of those names |
| `skills/investigate/` | `how`, `why` and its `epistemics.md`, the Investigation playbook |
| `skills/lead/` | `poteto-mode`: the playbooks, autonomy, the reply |
| `skills/audit/` | new: what a second model reads a diff for |

Not here yet: Hillclimb as a loop, Prototype, the forensics playbooks,
and everything about pull requests (Babysit, Shipping, Autopilot), which
in mad is yours to do from the sidebar.
