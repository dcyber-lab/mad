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
        let json = """
        {
          "machine": "Heng's MacBook Pro",
          "projects": [
            {"name": "mad", "agents": [
              {"id": "claude-3fe9", "name": "ios", "status": "working", "shown": "working"},
              {"id": "claude-91ab", "name": "kim", "status": "waiting", "shown": "needs-you"},
              {"id": "claude-77c0", "name": "gus", "status": "waiting", "shown": "needs-you"},
              {"id": "codex-2d", "name": "ada", "status": "idle", "shown": "done"}
            ]}
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
}
#else
import Foundation
import MadKit

enum Fixture {
    static let mac = Mac(name: "", baseURL: URL(string: "https://127.0.0.1:1")!, token: "")
    static func snapshot() -> DeckSnapshot? { nil }
}
#endif
