#if DEBUG
import Foundation
import MadKit

/// Debug builds only: `-fixture inbox` feeds the Mac screen a sample
/// snapshot, one item of each kind, so the cards can be looked at without a
/// live deck. The Mac is in memory only, and acts send nothing.
/// `-fixtureSkip N` leaves out the first N items.
enum Fixture {
    static let mac = Mac(name: "Studio", baseURL: URL(string: "https://127.0.0.1:1")!,
                         token: String(repeating: "0", count: 32))

    static func snapshot(now: Date = Date()) -> DeckSnapshot? {
        guard UserDefaults.standard.string(forKey: "fixture") == "inbox" else { return nil }
        let f = ISO8601DateFormatter()
        func ago(_ s: TimeInterval) -> String { f.string(from: now.addingTimeInterval(-s)) }
        func ahead(_ s: TimeInterval) -> String { f.string(from: now.addingTimeInterval(s)) }
        let json = """
        {
          "machine": "Heng's MacBook Pro",
          "quota": {
            "claude": {"five_hour": {"used": 42.4, "reset_at": "\(ahead(2 * 3600 + 10 * 60))"},
                       "seven_day": {"used": 71, "reset_at": "\(ahead(3 * 86400 + 5 * 3600))"}, "at": "\(ago(240))"},
            "codex": {"five_hour": {"used": 9, "reset_at": "\(ahead(4 * 3600 + 25 * 60))"},
                      "seven_day": {"used": 93.6, "reset_at": "\(ahead(5 * 86400))"}, "at": "\(ago(600))"}
          },
          "projects": [
            {"name": "mad", "agents": [
              {"id": "claude-3fe9", "kind": "claude", "name": "ios", "status": "running", "shown": "running", "mode": "window", "headless": true},
              {"id": "claude-91ab", "kind": "claude", "name": "kim", "status": "waiting", "shown": "waiting", "mode": "window", "headless": true},
              {"id": "claude-77c0", "kind": "claude", "name": "gus", "status": "waiting", "shown": "waiting", "mode": "window", "headless": true},
              {"id": "codex-2d", "kind": "codex", "name": "ada", "status": "idle", "shown": "finished", "headless": true},
              {"id": "claude-b1", "kind": "claude", "name": "builder", "status": "running", "shown": "running", "team": "ship", "mode": "window", "headless": true},
              {"id": "claude-r1", "kind": "claude", "name": "reviewer", "status": "idle", "shown": "idle", "team": "ship", "mode": "window", "headless": true}
            ]},
            {"name": "payments", "agents": [
              {"id": "pi-9", "kind": "pi", "status": "idle", "shown": "idle"},
              {"id": "sh-4", "kind": "shell", "title": "psql console", "status": "asleep", "shown": "asleep"}
            ]}
          ],
          "teams": [{"name": "ship", "project": "mad", "members": ["claude-b1", "claude-r1"]}],
          "states": [
            {"state": "waiting", "word": "Needs you", "tone": "needs"},
            {"state": "running", "word": "Working", "tone": "working"},
            {"state": "finished", "word": "Finished", "tone": "finished"},
            {"state": "idle", "word": "Idle", "tone": "quiet"},
            {"state": "asleep", "word": "Asleep", "tone": "quiet"},
            {"state": "starting", "word": "Starting", "tone": "quiet"},
            {"state": "stopped", "word": "Stopped", "tone": "quiet"}
          ],
          "inbox": {"needs": 6, "items": [
            {"key": "perm:1", "kind": "permission", "agent": "claude-91ab", "title": "kim", "context": "Bash",
             "text": "go test -race ./internal/desktop/...", "mono": true, "where": "~/go/src/mad",
             "since": "\(ago(140))", "blocks": true,
             "acts": [{"verb": "allow", "label": "Allow", "row": true, "primary": true},
                      {"verb": "always", "label": "Always allow"},
                      {"verb": "deny", "label": "Deny", "input": true}],
             "open": {"agent": "claude-91ab"}},
            {"key": "q:2", "kind": "question", "agent": "claude-77c0", "title": "gus", "context": "asks",
             "text": "Which database should the export read from?", "body": "The report can use the live replica or last night's snapshot.",
             "since": "\(ago(900))", "blocks": true,
             "acts": [{"verb": "answer", "label": "Live replica", "option": "Live replica"},
                      {"verb": "answer", "label": "Snapshot", "option": "Snapshot"},
                      {"verb": "answer", "label": "Something else", "input": true},
                      {"verb": "open", "label": "Open"}],
             "open": {"agent": "claude-77c0"}},
            {"key": "ask:3", "kind": "ask", "agent": "codex-2d", "title": "ada", "context": "in #plan",
             "text": "@you can I rename the payments package, or is that out of scope?",
             "since": "\(ago(3600))",
             "acts": [{"verb": "reply", "label": "Reply", "input": true, "primary": true},
                      {"verb": "open", "label": "Open"}],
             "open": {"channel": "plan"}},
            {"key": "skill:4", "kind": "skill", "icon": "skill", "title": "/csv-export", "context": "skill from gus",
             "text": "When parsing a CSV with quoted newlines, split on records, not lines, then trim the BOM.",
             "since": "\(ago(7200))",
             "acts": [{"verb": "approve", "label": "Approve", "primary": true},
                      {"verb": "discard", "label": "Discard"},
                      {"verb": "open", "label": "Open"}],
             "open": {"page": "skills"}},
            {"key": "budget:5", "kind": "budget", "icon": "budget", "title": "Budget", "context": "mad's own work",
             "text": "$4.60 of $5.00 spent today",
             "since": "\(ago(300))",
             "acts": [{"verb": "raise", "label": "Raise by $5", "option": "5"},
                      {"verb": "lift", "label": "Lift for today"}],
             "open": {"page": "board"}},
            {"key": "task:6", "kind": "task", "icon": "task", "title": "Ship the iOS beta", "context": "task is yours",
             "since": "\(ago(86400 * 2))",
             "acts": [{"verb": "open", "label": "Open", "primary": true}],
             "open": {"page": "board"}},
            {"key": "done:7", "kind": "finished", "agent": "claude-3fe9", "title": "ios", "context": "finished",
             "text": "Pairing by #pair= code is in; make ios is green and 38 tests pass.",
             "since": "\(ago(420))",
             "acts": [{"verb": "seen", "label": "Mark seen"}, {"verb": "open", "label": "Open"}],
             "open": {"agent": "claude-3fe9"}}
          ]}
        }
        """
        guard var snap = try? JSONDecoder().decode(DeckSnapshot.self, from: Data(json.utf8)) else { return nil }
        // `-fixtureSkip N` leaves out the first N items, to look at the ones below the fold.
        let skip = min(max(UserDefaults.standard.integer(forKey: "fixtureSkip"), 0), snap.inbox.count)
        snap.inbox = Array(snap.inbox.dropFirst(skip))
        snap.needs = max(0, snap.needs - skip)
        return snap
    }
    /// A sample conversation for any agent: a user message, markdown, a call
    /// with a result, a failed call, an image, and a message still being written.
    static func chat(for agentID: String, now: Date = Date()) -> (page: ChatPage, live: LiveMessage?)? {
        guard UserDefaults.standard.string(forKey: "fixture") == "inbox" else { return nil }
        let json = """
        {"file": "sample", "offset": 1, "reset": true, "entries": [
          {"kind": "user", "text": "Can you make the export read from the snapshot, and run the tests?"},
          {"kind": "text", "msg": "m1", "text": "Sure. I'll switch the **export** to read `snapshot.csv`, then run the suite.\\n\\n- update `export.go`\\n- run `go test ./...`"},
          {"kind": "tool", "tool": "Edit", "label": "internal/export/export.go", "id": "c1", "msg": "m1",
           "input": "- rows := db.Live()\\n+ rows := db.Snapshot()"},
          {"kind": "result", "id": "c1", "text": "File updated."},
          {"kind": "tool", "tool": "Bash", "label": "go test ./...", "id": "c2", "msg": "m1", "input": "go test ./..."},
          {"kind": "result", "id": "c2", "error": true,
           "text": "--- FAIL: TestExport (0.02s)\\n    export_test.go:41: want 12 rows, got 11\\nFAIL\\nFAIL\\texample/internal/export\\t0.31s"},
          {"kind": "text", "msg": "m2", "text": "One test fails: the snapshot has 11 rows and the test expects 12. I'll look at the fixture next."},
          {"kind": "user", "text": "Here is what I see on the report page.", "images": [{"mime": "image/png", "url": "/api/image?x=1"}]},
          {"kind": "user", "text": "ok, go ahead"},
          {"kind": "text", "msg": "m3", "text": "Looking now."},
          {"kind": "tool", "tool": "Read", "label": "internal/export/export_test.go", "id": "c3", "msg": "m3"}
        ]}
        """
        let liveJSON = """
        {"id": "m4", "version": 1, "done": false, "blocks": [
          {"kind": "thinking", "text": "The fixture lists twelve ids"},
          {"kind": "text", "text": "The test's fixture lists **twelve** ids, but the snapshot export drops the one with an empty name. That is the"}
        ]}
        """
        guard let page = try? JSONDecoder().decode(ChatPage.self, from: Data(json.utf8)) else { return nil }
        return (page, try? JSONDecoder().decode(LiveMessage.self, from: Data(liveJSON.utf8)))
    }
}
#else
import Foundation
import MadKit

enum Fixture {
    static let mac = Mac(name: "", baseURL: URL(string: "https://127.0.0.1:1")!, token: "")
    static func snapshot() -> DeckSnapshot? { nil }
    static func chat(for agentID: String) -> (page: ChatPage, live: LiveMessage?)? { nil }
}
#endif
