// How the conversation draws an agent's tool calls, tool by tool: what a
// call is (an icon, a title, what it is pointed at) and a body showing it
// as itself: a change as a diff, a command with what it printed, a todo
// list as a list, a plan as a document. After happy's knownTools and tool
// views (MIT License, https://github.com/slopus/happy), for the tools of
// claude, codex and pi.
"use strict";

// parseArgs reads a call's input: an object, else {}.
function parseArgs(raw) {
  if (raw && typeof raw === "object") return raw;
  if (typeof raw !== "string" || !raw) return {};
  try {
    const v = JSON.parse(raw);
    return v && typeof v === "object" && !Array.isArray(v) ? v : {};
  } catch (_) {
    return {};
  }
}

const str = (v) => (typeof v === "string" ? v : "");

// shortPath shows path p from dir when it is in it.
function shortPath(p, dir) {
  p = str(p);
  if (dir && p.startsWith(dir + "/")) return p.slice(dir.length + 1);
  return p;
}

const lineCount = (s) => (s ? s.replace(/\n$/, "").split("\n").length : 0);

// ---- the tools ----

// A view of a call: icon and title; sub, what it is pointed at (mono:
// a command or a path); stat, a node after it (+3 −1); body, a node
// under it; open, whether it starts unfolded; out, how its result shows:
// "show" (the default), "none" (only a failure) or "md".
const TOOLS = {
  Bash: (a) => ({
    icon: "terminal", title: str(a.description) || "Run", sub: firstLine(str(a.command)), mono: true,
    body: a.command && (str(a.command).includes("\n") || str(a.command).length > 90) ? codeBody("$ " + a.command) : null,
  }),
  bash: (a) => TOOLS.Bash(a), // pi
  shell: (a) => TOOLS.Bash({ command: codexCommand(a.command) }),
  exec_command: (a) => TOOLS.Bash({ command: str(a.cmd) }),
  local_shell: (a) => TOOLS.Bash({ command: codexCommand(a.action?.command) }),

  Read: (a, c) => ({ icon: "eye", title: "Read", sub: shortPath(a.file_path || a.path, c.dir) + readRange(a), mono: true, out: "lines" }),
  read: (a, c) => TOOLS.Read(a, c),
  NotebookRead: (a, c) => ({ icon: "eye", title: "Read", sub: shortPath(a.notebook_path, c.dir), mono: true }),
  view_image: (a, c) => ({ icon: "image", title: "View image", sub: shortPath(a.path, c.dir), mono: true }),

  Edit: (a, c) => editView(shortPath(a.file_path || a.path, c.dir), [{ old: a.old_string, new: a.new_string }]),
  edit: (a, c) => editView(shortPath(a.path || a.file_path, c.dir), // pi
    Array.isArray(a.edits) ? a.edits.map((e) => ({ old: e.oldText, new: e.newText })) : [{ old: a.oldText ?? a.old_string, new: a.newText ?? a.new_string }]),
  MultiEdit: (a, c) => editView(shortPath(a.file_path, c.dir), (Array.isArray(a.edits) ? a.edits : []).map((e) => ({ old: e.old_string, new: e.new_string }))),
  Write: (a, c) => writeView(shortPath(a.file_path || a.path, c.dir), str(a.content)),
  write: (a, c) => TOOLS.Write(a, c),
  NotebookEdit: (a, c) => ({
    icon: "edit", title: "Edit notebook", sub: shortPath(a.notebook_path, c.dir), mono: true,
    body: a.new_source ? codeBody(a.new_source) : null, out: "none",
  }),
  apply_patch: (a, c) => patchView(str(a.input) || str(a.patch), c),
  exec: (a, c) => execView(str(a.input), c), // codex's script of tool calls

  Glob: (a, c) => ({ icon: "search", title: "Find files", sub: str(a.pattern) + inPath(a.path, c), mono: true }),
  find: (a, c) => TOOLS.Glob(a, c),
  Grep: (a, c) => ({ icon: "search", title: "Search", sub: str(a.pattern) + inPath(a.path || a.glob, c), mono: true }),
  grep: (a, c) => TOOLS.Grep(a, c),
  LS: (a, c) => ({ icon: "folder", title: "List", sub: shortPath(a.path, c.dir) || ".", mono: true }),
  ls: (a, c) => TOOLS.LS(a, c),

  WebFetch: (a) => ({ icon: "web", title: "Fetch", sub: urlShown(a.url), mono: true }),
  WebSearch: (a) => ({ icon: "web", title: "Search the web", sub: str(a.query) }),
  web_search: (a) => TOOLS.WebSearch(a),
  web_search_call: (a) => TOOLS.WebSearch(a),

  TodoWrite: (a) => todoView((Array.isArray(a.todos) ? a.todos : []).map((t) => ({ text: t.content || t.activeForm, status: t.status }))),
  update_plan: (a) => todoView((Array.isArray(a.plan) ? a.plan : []).map((t) => ({ text: t.step, status: t.status })), str(a.explanation)),

  // claude's task list, as its later versions keep it.
  TaskCreate: (a) => ({ icon: "todo", title: "Add task", sub: str(a.subject) || str(a.description), out: "none" }),
  TaskUpdate: (a) => ({ icon: "todo", title: "Update task", sub: [str(a.subject), str(a.status)].filter(Boolean).join(" · "), out: "none" }),
  TaskList: () => ({ icon: "todo", title: "Tasks", sub: "" }),
  Task: (a) => ({
    icon: "agent", title: str(a.description) || "Subagent", sub: str(a.subagent_type),
    body: a.prompt ? mdBody(a.prompt, "prompt") : null, out: "md",
  }),
  Agent: (a) => TOOLS.Task(a),
  ExitPlanMode: (a) => ({ icon: "plan", title: "Plan", sub: "", body: a.plan ? mdBody(a.plan) : null, open: !!a.plan, out: "none" }),
  exit_plan_mode: (a) => TOOLS.ExitPlanMode(a),
  AskUserQuestion: (a) => questionView(Array.isArray(a.questions) ? a.questions : []),
  Skill: (a) => ({ icon: "skill", title: "Skill", sub: str(a.skill) || str(a.command) }),
  ToolSearch: (a) => ({ icon: "tool", title: "Load tools", sub: str(a.query) }),
};

// toolView is how a call to tool with args shows; label is what the
// deck read it as, for what no view knows.
function toolView(tool, args, ctx) {
  const make = TOOLS[tool];
  let v = null;
  if (make) {
    try { v = make(args, ctx); } catch (err) { console.error("tool view", tool, err); }
  } else if (/^mcp__/.test(tool || "")) {
    // mcp__server__tool
    const [, server, name] = /^mcp__(.+?)__(.+)$/.exec(tool) || [];
    v = { icon: "plug", title: (name || tool).replace(/_/g, " "), sub: server || "" };
    if (Object.keys(args).length) v.body = codeBody(JSON.stringify(args, null, 2));
  }
  return v;
}

// callCard is a call's row in the conversation, its body under it to
// unfold. label and input are the deck's reading of it, for a tool with
// no view or a call whose input has not all come yet.
function callCard(tool, args, ctx, label, input) {
  const v = toolView(tool, args || {}, ctx || {}) || {};
  const d = el("details", "call");
  d.view = v;
  const s = el("summary");
  const st = el("span", "st");
  st.append(el("span", "ring"), el("span", "spin"), icon("check", "ok"), icon("x", "bad"));
  const sub = v.sub || label || "";
  s.append(icon(v.icon || toolIcon(tool), "ti"), el("span", "tool", v.title || tool), el("span", `label${v.mono && v.sub ? " mono" : ""}`, sub));
  if (v.stat) s.append(v.stat);
  s.append(st);
  d.append(s);
  if (v.body) d.append(v.body);
  else if (input && !v.title) d.append(el("pre", "input", input));
  if (v.open) d.open = true;
  return d;
}

// callResult adds what call d gave back to it: text, and images (their
// URLs), which open the call to be seen.
function callResult(d, text, error, images) {
  if (images?.length) {
    if (text || error) d.append(el("pre", `output${error ? " err" : ""}`, text || "(no output)"));
    const box = el("div", "cbody result");
    box.append(imageRow(images));
    d.append(box);
    d.classList.add(error ? "failed" : "done");
    d.open = true;
    return;
  }
  if (d.view?.exec) {
    // What codex's script printed, after how it went.
    const m = /^Script (completed|failed)\n(?:Wall time[^\n]*\n)?(?:Output:\n)?/.exec(text || "");
    if (m) {
      error = error || m[1] === "failed";
      text = text.slice(m[0].length);
    }
  }
  d.classList.add(error ? "failed" : "done");
  const how = d.view?.out || "show";
  if (!error && how === "none") return;
  if (!error && how === "md" && text) {
    const box = el("div", "cbody result");
    box.append(md(text));
    d.append(box);
    return;
  }
  if (!error && how === "lines" && numbered(d, text)) return;
  d.append(el("pre", `output${error ? " err" : ""}`, text || "(no output)"));
}

// numbered adds a file's lines as Read gives them ("  12→text") to d,
// the numbers in a gutter; false when text is not that.
function numbered(d, text) {
  const cut = (text || "").indexOf("\n\n<system-reminder>");
  const lines = (cut < 0 ? text || "" : text.slice(0, cut)).replace(/\n$/, "").split("\n");
  const re = /^\s*(\d+)→(.*)$/;
  if (!lines.length || !re.test(lines[0])) return false;
  const pre = el("pre", "output lines");
  for (const line of lines) {
    const m = re.exec(line);
    const row = el("span", "ln");
    if (m) row.append(el("i", "", m[1]), m[2] + "\n");
    else row.append(el("i", "", ""), line + "\n");
    pre.append(row);
  }
  d.append(pre);
  return true;
}

// toolIcon picks an icon for a tool no view knows, by its name.
function toolIcon(tool) {
  const t = (tool || "").toLowerCase();
  if (/bash|shell|exec|command|terminal/.test(t)) return "terminal";
  if (/web|fetch|browser|url/.test(t)) return "web";
  if (/edit|write|patch|notebook/.test(t)) return "edit";
  if (/^(read|view|cat)/.test(t)) return "eye";
  if (/grep|glob|search|^find|^ls$|list/.test(t)) return "search";
  if (/task|agent/.test(t)) return "agent";
  if (/todo|plan/.test(t)) return "todo";
  return "tool";
}

// ---- codex's scripts ----

// execView: a script of codex's (its exec tool) drawn as the tool it
// calls, when it calls one; else as the script.
function execView(code, c) {
  const calls = scriptCalls(code);
  if (calls.length === 1 && calls[0].args) {
    const v = toolView(calls[0].name, calls[0].args, c);
    if (v) return { ...v, exec: true };
  }
  const sub = calls.length ? calls.map((x) => x.name).join(", ") : firstLine(code.trim());
  return { icon: "terminal", title: "Script", sub, mono: true, body: code.trim() ? codeBody(code.trim()) : null, exec: true };
}

// scriptCalls finds the tools.NAME(ARGS) calls in a script, each with
// its arguments read as JSON where they can be: a string (a patch) as
// {input}, an object as it is.
function scriptCalls(code) {
  const out = [];
  const re = /\btools\.(\w+)\s*\(/g;
  let m;
  while ((m = re.exec(code))) {
    const end = closing(code, re.lastIndex - 1);
    if (end < 0) break;
    out.push({ name: m[1], args: scriptArgs(code.slice(re.lastIndex, end).trim()) });
    re.lastIndex = end + 1;
  }
  return out;
}

// closing is where the bracket at open in s closes, past strings; -1
// if it does not.
function closing(s, open) {
  let depth = 0;
  for (let i = open; i < s.length; i++) {
    const ch = s[i];
    if (ch === '"' || ch === "'" || ch === "`") {
      for (i++; i < s.length && s[i] !== ch; i++) if (s[i] === "\\") i++;
    } else if ("([{".includes(ch)) {
      depth++;
    } else if (")]}".includes(ch) && --depth === 0) {
      return i;
    }
  }
  return -1;
}

// scriptArgs reads a call's argument: JSON, or a script's object with
// its keys bare; null when it is neither.
function scriptArgs(t) {
  if (!t) return {};
  if (t[0] === '"') {
    try { return { input: JSON.parse(t) }; } catch (_) { return null; }
  }
  if (t[0] !== "{") return null;
  try { return JSON.parse(t); } catch (_) { /* keys bare */ }
  let out = "";
  for (let i = 0; i < t.length; i++) {
    const ch = t[i];
    if (ch === '"') {
      const j = closingQuote(t, i);
      out += t.slice(i, j + 1);
      i = j;
    } else if (/[A-Za-z_$]/.test(ch) && /[{,]\s*$/.test(out)) {
      const k = /^[\w$]+/.exec(t.slice(i))[0];
      const rest = t.slice(i + k.length);
      if (/^\s*:/.test(rest)) out += JSON.stringify(k);
      else out += k;
      i += k.length - 1;
    } else {
      out += ch;
    }
  }
  try { return JSON.parse(out.replace(/,(\s*[}\]])/g, "$1")); } catch (_) { return null; }
}

function closingQuote(s, i) {
  for (i++; i < s.length && s[i] !== '"'; i++) if (s[i] === "\\") i++;
  return i;
}

// ---- what the views are made of ----

function firstLine(s) {
  const i = s.indexOf("\n");
  return i < 0 ? s : s.slice(0, i) + " …";
}

// codexCommand is the command line codex's shell runs: the script of a
// shell -c, else the words joined.
function codexCommand(cmd) {
  if (typeof cmd === "string") return cmd;
  if (!Array.isArray(cmd)) return "";
  if (cmd.length === 3 && /(^|\/)(ba|z)?sh$/.test(cmd[0]) && /^-l?c$/.test(cmd[1])) return cmd[2];
  return cmd.map((w) => (/^[\w./:=@%+,-]+$/.test(w) ? w : JSON.stringify(w))).join(" ");
}

function readRange(a) {
  const from = Number(a.offset) || 0;
  const n = Number(a.limit) || 0;
  if (!from && !n) return "";
  return n ? ` · ${from || 1}–${(from || 1) + n - 1}` : ` · from ${from}`;
}

function inPath(p, c) {
  const s = shortPath(p, c.dir);
  return s ? `  in ${s}` : "";
}

function urlShown(u) {
  try {
    const x = new URL(str(u));
    return x.host + (x.pathname === "/" ? "" : x.pathname);
  } catch (_) {
    return str(u);
  }
}

function codeBody(text) {
  return el("pre", "input", text);
}

function mdBody(text, cls) {
  const box = el("div", `cbody${cls ? " " + cls : ""}`);
  box.append(md(str(text)));
  return box;
}

function stat(added, deleted) {
  const s = el("span", "stat");
  if (added) s.append(el("span", "plus", `+${added}`));
  if (deleted) s.append(el("span", "minus", `−${deleted}`));
  return added || deleted ? s : null;
}

// editView: a change of old text to new in file, a diff each.
function editView(file, edits) {
  const box = el("div", "diff");
  let added = 0, deleted = 0, lines = 0;
  edits.forEach((e, i) => {
    const ops = lineDiff(str(e.old), str(e.new));
    if (i) box.append(el("div", "dl h", "⋯"));
    for (const op of ops) {
      if (op.t === "+") added++;
      if (op.t === "-") deleted++;
    }
    lines += ops.length;
    drawOps(box, ops);
  });
  // A call whose input has not come yet: nothing to show.
  if (!lines) return { icon: "edit", title: "Edit", sub: file, mono: true, out: "none" };
  return { icon: "edit", title: "Edit", sub: file, mono: true, stat: stat(added, deleted), body: box, open: lines <= 40, out: "none" };
}

function writeView(file, content) {
  const n = lineCount(content);
  return {
    icon: "fileplus", title: "Write", sub: file, mono: true, stat: stat(n, 0),
    body: content ? codeBody(content) : null, open: false, out: "none",
  };
}

// patchView: codex's patch, a diff for each file it changes.
function patchView(patch, c) {
  const files = parsePatch(patch);
  if (!files.length) return { icon: "edit", title: "Edit", sub: "", body: patch ? codeBody(patch) : null, out: "none" };
  const box = el("div", "diff");
  let added = 0, deleted = 0, lines = 0;
  for (const f of files) {
    if (files.length > 1) {
      const h = el("div", "fh");
      h.append(el("span", "", shortPath(f.path, c.dir)), stat(f.added, f.deleted) || "");
      box.append(h);
    }
    drawOps(box, f.ops);
    added += f.added;
    deleted += f.deleted;
    lines += f.ops.length;
  }
  const first = shortPath(files[0].path, c.dir);
  const sub = files.length > 1 ? `${first} and ${files.length - 1} more` : first;
  const verb = files.length === 1 && files[0].kind === "add" ? "Write" : files.length === 1 && files[0].kind === "delete" ? "Delete" : "Edit";
  return { icon: verb === "Write" ? "fileplus" : "edit", title: verb, sub, mono: true, stat: stat(added, deleted), body: box, open: lines <= 40, out: "none" };
}

// parsePatch reads codex's patch format (*** Update File: …) or a
// unified diff into files, each with its lines as diff ops.
function parsePatch(text) {
  const files = [];
  let f = null;
  const start = (path, kind) => {
    f = { path, kind, ops: [], added: 0, deleted: 0 };
    files.push(f);
  };
  for (const line of str(text).split("\n")) {
    let m;
    if ((m = /^\*\*\* (Add|Update|Delete) File: (.+)$/.exec(line))) {
      start(m[2], m[1].toLowerCase());
    } else if ((m = /^\*\*\* Move to: (.+)$/.exec(line)) && f) {
      f.path = `${f.path} → ${m[1]}`;
    } else if ((m = /^diff --git a\/(.+?) b\/(.+)$/.exec(line))) {
      start(m[2], "update");
    } else if (/^(\*\*\* (Begin|End) Patch|\*\*\* End of File|index |--- |\+\+\+ |new file mode|deleted file mode|similarity |rename (from|to) )/.test(line)) {
      if (f && /^new file mode/.test(line)) f.kind = "add";
      if (f && /^deleted file mode/.test(line)) f.kind = "delete";
    } else if (line.startsWith("@@")) {
      if (!f) start("", "update");
      // "@@ -3,7 +3,8 @@ func f": where it is, by the code around it
      // or by its line; "@@ func f" in codex's patches.
      const u = /^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@ ?(.*)$/.exec(line);
      f.ops.push({ t: "@", s: u ? u[2] || `line ${u[1]}` : line.replace(/^@@ ?/, "") || "⋯" });
    } else if (f && /^[+\- ]/.test(line)) {
      const t = line[0] === " " ? " " : line[0];
      f.ops.push({ t, s: line.slice(1) });
      if (t === "+") f.added++;
      if (t === "-") f.deleted++;
    }
  }
  return files.filter((x) => x.ops.length || x.kind !== "update");
}

// lineDiff is old text a against new b, line by line: ops of " " (both),
// "-" (a's) and "+" (b's).
function lineDiff(a, b) {
  const A = a ? a.split("\n") : [];
  const B = b ? b.split("\n") : [];
  let pre = 0;
  while (pre < A.length && pre < B.length && A[pre] === B[pre]) pre++;
  let suf = 0;
  while (suf < A.length - pre && suf < B.length - pre && A[A.length - 1 - suf] === B[B.length - 1 - suf]) suf++;
  const a2 = A.slice(pre, A.length - suf);
  const b2 = B.slice(pre, B.length - suf);
  const ops = A.slice(0, pre).map((s) => ({ t: " ", s }));
  const n = a2.length, m = b2.length;
  if (n * m > 250000) {
    // Too long to match up: all of a's, then all of b's.
    for (const s of a2) ops.push({ t: "-", s });
    for (const s of b2) ops.push({ t: "+", s });
  } else {
    // Longest common subsequence, from the end.
    const L = Array.from({ length: n + 1 }, () => new Uint32Array(m + 1));
    for (let i = n - 1; i >= 0; i--) {
      for (let j = m - 1; j >= 0; j--) {
        L[i][j] = a2[i] === b2[j] ? L[i + 1][j + 1] + 1 : Math.max(L[i + 1][j], L[i][j + 1]);
      }
    }
    let i = 0, j = 0;
    while (i < n || j < m) {
      if (i < n && j < m && a2[i] === b2[j]) { ops.push({ t: " ", s: a2[i] }); i++; j++; }
      else if (j < m && (i >= n || L[i][j + 1] >= L[i + 1][j])) { ops.push({ t: "+", s: b2[j] }); j++; }
      else { ops.push({ t: "-", s: a2[i] }); i++; }
    }
    // Within a run of changes, a's lines before b's.
    for (let k = pre; k < ops.length; k++) {
      if (ops[k].t !== "+" && ops[k].t !== "-") continue;
      let e = k;
      while (e < ops.length && ops[e].t !== " ") e++;
      const run = ops.slice(k, e);
      ops.splice(k, run.length, ...run.filter((o) => o.t === "-"), ...run.filter((o) => o.t === "+"));
      k = e;
    }
  }
  for (const s of A.slice(A.length - suf)) ops.push({ t: " ", s });
  return ops;
}

// drawOps adds diff ops to box as lines; a run of changes with as many
// lines out as in marks what changed within each pair of lines.
function drawOps(box, ops) {
  for (let k = 0; k < ops.length; k++) {
    const op = ops[k];
    if (op.t === "@") {
      box.append(el("div", "dl h", op.s));
      continue;
    }
    if (op.t === "-") {
      let e = k;
      while (e < ops.length && ops[e].t === "-") e++;
      let f = e;
      while (f < ops.length && ops[f].t === "+") f++;
      if (f - e === e - k && e - k <= 8) {
        const lines = [];
        for (let x = 0; x < e - k; x++) {
          const [d, a] = pairMarks(ops[k + x].s, ops[e + x].s);
          lines[x] = d;
          lines[e - k + x] = a;
        }
        box.append(...lines);
        k = f - 1;
        continue;
      }
    }
    box.append(diffLine(op.t, op.s));
  }
}

function diffLine(t, s) {
  const d = el("div", `dl ${t === "+" ? "a" : t === "-" ? "d" : "c"}`, s || " ");
  d.dataset.s = t === " " ? "" : t === "-" ? "−" : "+";
  return d;
}

// pairMarks draws a line changed to another, what differs marked.
function pairMarks(a, b) {
  let p = 0;
  while (p < a.length && p < b.length && a[p] === b[p]) p++;
  let s = 0;
  while (s < a.length - p && s < b.length - p && a[a.length - 1 - s] === b[b.length - 1 - s]) s++;
  const mark = (t, x) => {
    const d = diffLine(t, "");
    d.textContent = "";
    const mid = x.slice(p, x.length - s);
    // Marking nearly all of a line says nothing.
    if (!mid || mid.length > x.length * 0.7) {
      d.textContent = x || " ";
      return d;
    }
    d.append(x.slice(0, p), el("span", "x", mid), x.slice(x.length - s));
    return d;
  };
  return [mark("-", a), mark("+", b)];
}

// todoView: the agent's todo list, what is being done marked.
function todoView(items, note) {
  const list = el("ul", "todos");
  let done = 0;
  for (const t of items) {
    const st = t.status === "completed" ? "done" : t.status === "in_progress" ? "now" : "todo";
    if (st === "done") done++;
    const li = el("li", st);
    li.append(icon(st === "done" ? "circlecheck" : st === "now" ? "circledot" : "circle"), el("span", "", str(t.text)));
    list.append(li);
  }
  let body = items.length ? list : null;
  if (note) {
    body = el("div", "");
    body.append(el("div", "todonote", note), list);
  }
  return { icon: "todo", title: "Tasks", sub: items.length ? `${done} of ${items.length} done` : "", body, open: !!body, out: "none", todos: items };
}

// questionView: what the agent asked, its options under each question.
function questionView(qs) {
  const box = el("div", "cbody quests");
  for (const q of qs) {
    const b = el("div", "qv");
    b.append(el("div", "qq", str(q.question)));
    const opts = (Array.isArray(q.options) ? q.options : []).map((o) => str(o.label)).filter(Boolean);
    if (opts.length) b.append(el("div", "qo muted", opts.join(" · ")));
    box.append(b);
  }
  return { icon: "question", title: "Asked", sub: str(qs[0]?.question), body: qs.length ? box : null };
}
