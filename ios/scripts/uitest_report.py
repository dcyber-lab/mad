#!/usr/bin/env python3
"""Makes the UI-test report: every screenshot of every run, grouped by case, with
a pass or fail and the failure message. Reads the .xcresult bundles in a results
directory (Xcode's xcresulttool) and writes <out>/index.html with the images beside it.

usage: uitest_report.py <results-dir> <out-dir>
"""
import glob
import html
import json
import os
import shutil
import subprocess
import sys
import tempfile


def tool(*args):
    return subprocess.run(["xcrun", "xcresulttool", *args], capture_output=True, text=True)


def walk(nodes, path=()):
    """Yields (path of node names, node) for every node below `nodes`."""
    for n in nodes or []:
        yield path, n
        yield from walk(n.get("children"), path + (n.get("name", ""),))


def test_cases(bundle):
    """The bundle's test cases: {identifier: {name, class, result, failures[], duration}}."""
    out = tool("get", "test-results", "tests", "--path", bundle)
    try:
        data = json.loads(out.stdout)
    except ValueError:
        return {}
    cases = {}
    for _, n in walk(data.get("testNodes")):
        if n.get("nodeType") != "Test Case":
            continue
        ident = n.get("nodeIdentifier") or n.get("name", "")
        failures = [c.get("name", "") for _, c in walk(n.get("children")) if c.get("nodeType") == "Failure Message"]
        cases[ident] = {
            "name": n.get("name", ident).rstrip("()"),
            "class": ident.split("/")[0],
            "result": n.get("result", "Unknown"),
            "failures": failures,
            "duration": n.get("durationInSeconds"),
        }
    return cases


def attachments(bundle, dest):
    """Exports the bundle's attachments into `dest`; {test identifier: [(name, file)]}."""
    os.makedirs(dest, exist_ok=True)
    tool("export", "attachments", "--path", bundle, "--output-path", dest)
    try:
        manifest = json.load(open(os.path.join(dest, "manifest.json")))
    except (OSError, ValueError):
        return {}
    out = {}
    for entry in manifest:
        ident = entry.get("testIdentifier") or entry.get("testIdentifierURL", "")
        for a in entry.get("attachments", []):
            name = a.get("suggestedHumanReadableName") or a.get("exportedFileName", "")
            # "name_0_UUID.png" -> "name"
            human = name.rsplit("_", 2)[0] if name.count("_") >= 2 and name.endswith((".png", ".jpeg", ".jpg")) else name
            out.setdefault(ident, []).append((human, a.get("exportedFileName", ""), a.get("timestamp", 0)))
    return out


def main():
    results, out = sys.argv[1], sys.argv[2]
    os.makedirs(out, exist_ok=True)
    img_dir = os.path.join(out, "img")
    os.makedirs(img_dir, exist_ok=True)
    groups = {}  # class -> [case dict]
    n_total = n_fail = 0
    for bundle in sorted(glob.glob(os.path.join(results, "*.xcresult"))):
        tmp = tempfile.mkdtemp(prefix="madui-")
        try:
            cases = test_cases(bundle)
            atts = attachments(bundle, tmp)
            for ident, c in cases.items():
                pics = []
                for human, fname, ts in sorted(atts.get(ident, []), key=lambda t: t[2]):
                    src = os.path.join(tmp, fname)
                    if not os.path.isfile(src):
                        continue
                    dst_name = f"{os.path.basename(bundle)[:-9]}-{c['name']}-{len(pics)}{os.path.splitext(fname)[1] or '.png'}"
                    shutil.copy(src, os.path.join(img_dir, dst_name))
                    pics.append((human, f"img/{dst_name}"))
                c["pics"] = pics
                groups.setdefault(c["class"], []).append(c)
                n_total += 1
                n_fail += c["result"] != "Passed"
        finally:
            shutil.rmtree(tmp, ignore_errors=True)

    rows = []
    for klass in sorted(groups):
        cs = groups[klass]
        ok = all(c["result"] == "Passed" for c in cs)
        rows.append(f'<section><h2>{"✅" if ok else "❌"} {html.escape(klass)}</h2>')
        for c in sorted(cs, key=lambda c: c["name"]):
            passed = c["result"] == "Passed"
            rows.append(f'<article class="{"pass" if passed else "fail"}"><h3>{"✅" if passed else "❌"} {html.escape(c["name"])}'
                        f'<small>{c["result"]}</small></h3>')
            for f in c["failures"]:
                rows.append(f'<pre class="msg">{html.escape(f)}</pre>')
            rows.append('<div class="pics">')
            for human, path in c["pics"]:
                rows.append(f'<figure><a href="{html.escape(path)}"><img src="{html.escape(path)}" loading="lazy"></a>'
                            f'<figcaption>{html.escape(human)}</figcaption></figure>')
            if not c["pics"]:
                rows.append('<p class="none">no screenshot</p>')
            rows.append('</div></article>')
        rows.append('</section>')
    summary = f'{n_total - n_fail} of {n_total} cases passed' if n_total else 'no results'
    page = f"""<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>mad UI tests</title>
<style>
:root {{ color-scheme: light dark; --bg:#fff; --fg:#111; --mut:#666; --line:#ddd; --bad:#d11; }}
@media (prefers-color-scheme: dark) {{ :root {{ --bg:#111; --fg:#eee; --mut:#999; --line:#333; --bad:#f66; }} }}
body {{ margin:0; padding:24px; background:var(--bg); color:var(--fg); font:16px/1.4 -apple-system, "PingFang SC", sans-serif; }}
h1 {{ margin:0 0 4px }} .sum {{ color:var(--mut); margin-bottom:24px }}
section {{ margin:0 0 36px }} h2 {{ border-bottom:1px solid var(--line); padding-bottom:6px }}
article {{ margin:16px 0 }} h3 {{ margin:0 0 6px; font-size:17px }} h3 small {{ color:var(--mut); font-weight:400; margin-left:8px }}
article.fail h3 {{ color:var(--bad) }}
.msg {{ background:rgba(209,17,17,.1); color:var(--bad); padding:8px 10px; border-radius:6px; white-space:pre-wrap; margin:6px 0 }}
.pics {{ display:flex; gap:14px; flex-wrap:wrap; align-items:flex-start }}
figure {{ margin:0; width:220px }} figure img {{ width:220px; border:1px solid var(--line); border-radius:10px; display:block }}
figcaption {{ font-size:13px; color:var(--mut); margin-top:4px }} .none {{ color:var(--mut) }}
</style>
<h1>mad UI tests</h1><div class="sum">{html.escape(summary)}</div>
{''.join(rows)}"""
    open(os.path.join(out, "index.html"), "w", encoding="utf-8").write(page)
    print(f"report: {summary}")


if __name__ == "__main__":
    main()
