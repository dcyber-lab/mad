import Foundation

/// The confirm card of a bot's request to create an agent or a team, as the person sees it: every word is already worded
/// (the mapping in MadShell translates by the request's type and the act's verb, never by the server's English).
public struct CreateCard: Equatable, Sendable {
    /// One row of the spec box: a role and its model words (a team), or "模型" and the model, "加入群聊" and the channel (an agent).
    public struct Row: Equatable, Sendable, Identifiable {
        public var id: String
        public var left: String
        /// "负责人" after the left text, a small grey tag; empty for none.
        public var tag: String
        public var right: String
        public init(id: String, left: String, tag: String = "", right: String) {
            self.id = id; self.left = left; self.tag = tag; self.right = right
        }
    }

    /// The item's key: what an act is sent against.
    public var id: String
    /// 想创建一个团队 / 想创建一个 agent; in a channel, with the asker's label in front.
    public var head: String
    public var askerID: String
    public var askerKind: String
    public var name: String
    /// 项目 mad
    public var project: String
    public var rows: [Row]
    /// 第一件事 / 第一句话, and the text; both empty when there is none.
    public var firstLabel: String
    public var first: String
    /// 刚刚 / 等了 3 分钟
    public var waited: String
    /// The act ids the item gave: allow, deny, and deny with words (改一下…).
    public var allow: String?
    public var deny: String?
    public var change: String?

    public init(id: String, head: String, askerID: String = "", askerKind: String = "", name: String, project: String = "",
                rows: [Row] = [], firstLabel: String = "", first: String = "", waited: String = "",
                allow: String? = nil, deny: String? = nil, change: String? = nil) {
        self.id = id; self.head = head; self.askerID = askerID; self.askerKind = askerKind; self.name = name; self.project = project
        self.rows = rows; self.firstLabel = firstLabel; self.first = first; self.waited = waited
        self.allow = allow; self.deny = deny; self.change = change
    }

    public var accessibilityLabel: String { [head, name, project].filter { !$0.isEmpty }.joined(separator: ", ") }
}

/// What the person's answer to a create card looks like in the chat: mad's own line, not the bot's words.
public struct CreateLine: Equatable, Sendable, Identifiable {
    public enum Phase: Equatable, Sendable {
        /// mad is starting it: a spinner where the tile goes, no sub, no 打开.
        case creating
        /// Made: the tile or avatar, the title and sub, and a 打开 pill (the whole line is the button).
        case created
        /// Declined, changes, expired, failed: one centred grey line.
        case note
    }

    /// Where 打开 goes: a channel or an agent. Nil when what was made is gone (the sub says so).
    public enum Target: Equatable, Sendable {
        case channel(String)
        case agent(String)
    }

    /// The tile or avatar of a made thing.
    public enum Mark: Equatable, Sendable {
        case team(members: [Member])
        case agent(id: String, kind: String, state: CharacterState)
    }

    public struct Member: Equatable, Sendable {
        public var id: String
        public var kind: String
        public var state: CharacterState
        public init(id: String, kind: String, state: CharacterState = .idle) { self.id = id; self.kind = kind; self.state = state }
    }

    public var id: String
    public var phase: Phase
    /// The title ("团队「发布检查」建好了") or, for a note, the whole line.
    public var title: String
    public var sub: String
    public var mark: Mark?
    public var target: Target?

    public init(id: String, phase: Phase, title: String, sub: String = "", mark: Mark? = nil, target: Target? = nil) {
        self.id = id; self.phase = phase; self.title = title; self.sub = sub; self.mark = mark; self.target = target
    }

    public var accessibilityLabel: String {
        phase == .created && target != nil ? title + "，" + L("打开") : title
    }
}
