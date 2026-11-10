---
name: investigate
description: Answer a question about code without changing it. How something works, why it was built this way, whether we are sure, which of two ways to take. Every claim cited, and what is known kept apart from what is inferred. For a role that researches.
---

# Investigate

An investigation is read-only. It ends in a cited explanation or a recommendation, not in a code change. You own the answer.

## Frame the question

Say the question back in your own words, and what the person will do with the answer. If its scope is unclear, state your reading and go on: the person can redirect.

A question that carries its own answer ("we do it this way for performance, right?") is a hypothesis to test, not a conclusion to confirm. Treat it as one candidate among others. If the evidence says the premise is wrong, say so.

## How does it work

Explain at the level of a senior engineer joining the subsystem: enough to build a working model, not annotated source.

- **A narrow question** (one function, one module): read it and its callers, and answer.
- **A subsystem** (several files or services, a feature that cuts across): split the question into two to four angles, each a distinct slice. For each, trace the components, the flow step by step with the data that passes between steps, the boundaries to the rest of the code, and what surprised you. Then join the slices into one picture. Where two slices disagree, check the code yourself.

Write the answer in these sections. Drop one that has nothing to say.

- **Overview.** One or two paragraphs: what this is, what it does, why it exists. A reader decides here whether to go on.
- **Key concepts.** The types, services and abstractions needed to follow the rest. Short definitions.
- **How it works.** The longest section. What triggers the flow, what happens step by step, where data goes, where the decisions are. Prose, with the files and functions named. Add a diagram when components talk to each other or data changes shape in stages, and none when prose carries it.
- **Where things live.** The files and directories someone needs to start working here.
- **Gotchas.** What surprises, what history explains, what is easy to get wrong.

Use concrete names: "`UserService` calls `AuthClient.refresh()`", not "the service delegates to the client". When something is complex, say why it is. When it is simple, do not pad it.

## Why is it this way

The code shows what it does, not why it exists. The why is in commits, pull requests, issues, documents and conversations, all incomplete. Read `./epistemics.md` before you write a why answer, and follow it.

1. **Anchor in the code.** The files and line ranges, the key symbols, and the commits that last touched them: `git blame -L <start>,<end> <file>`, `git log --follow -p -- <file>`, `git log --oneline -20 -- <file>`. Pull request numbers stand in merge subjects as `(#1234)`.
2. **Read every source you can reach**, and note each one you could not:
   - Source control: the commit messages, the pull requests and their review threads (`gh pr view <number> --json title,body,comments,reviews`), code comments, the tests added with the change.
   - The issue tracker, design documents, team chat, monitoring and error tracking, where you have a tool that reaches them.
   - For defensive code (a retry, a timeout, a nil check, a flag), look for the incident that made someone add it.
3. **Sort every claim by its evidence**: direct, supported, inferred, speculative, unknown. The tier sets the wording.
4. **Do not let the newest commit speak for the whole.** The present shape is often many earlier decisions piled up. Trace back.

Write the answer in these sections:

- **The question**, restated.
- **The code in question.** Files, line ranges, symbols.
- **What we found.** Claims with textual evidence, each cited: a pull request, an issue, a commit, a `file:line`.
- **What we can reasonably infer.** Each with its chain shown ("given A and B, C is likely"), in hedged words.
- **Competing hypotheses**, when the evidence fits more than one story. For each: the evidence for, and what is against or missing.
- **What we don't know.** The specific gaps: what you searched, with which words, and found nothing.
- **Sources consulted.** One line per source, those that gave nothing and those you could not reach included.
- **Confidence.** One or two sentences.

If the question comes before a change to this code, end with what the history says to preserve, to change, to avoid, and where the risk is.

## Which should we do

A decision between alternatives gets a recommendation and a table of tradeoffs.

- If the deciding fact is one you can observe by running something (behavior, timing, output), run it instead of arguing it. A throwaway script in a temporary directory is enough. Say that it is throwaway.
- Give your real judgment with its reasons. Do not list pros and cons and stop. "No" and "neither" are answers.

## Before you send it

- Every claim carries its evidence or its label in the same sentence: measured, cited, inferred, or guess. A prediction is a guess.
- Open every `file:line` you cite, once more. Never cite a link, a pull request or a line you did not read.
- Never hand the reader a check you could have run.
- Short declarative sentences. Lead with the answer, then the detail.
