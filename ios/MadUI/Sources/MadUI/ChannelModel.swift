import Foundation

// What the channel view draws: plain structs, nothing of MadKit. The shell maps a channel's
// messages and its deck onto them (MadShell/ChannelMapping.swift); this view only draws.

/// A member of the channel (or the sender of a run of messages).
public struct ChannelMember: Identifiable, Equatable, Sendable {
    public var id: String
    public var kind: String
    public var state: CharacterState
    public var name: String
    /// How it stands, in a words of its own: "在工作 · Bash go test", "空闲".
    public var status: String
    public var isLead: Bool
    /// Its role in the channel's team ("架构师"); empty for a helper or a channel without a team.
    public var role: String
    /// A role with no agent behind it (not started, or gone): a faded row that opens nothing.
    public var dim: Bool

    public init(id: String, kind: String, state: CharacterState, name: String, status: String = "", isLead: Bool = false, role: String = "",
                dim: Bool = false) {
        self.id = id; self.kind = kind; self.state = state; self.name = name; self.status = status; self.isLead = isLead; self.role = role
        self.dim = dim
    }
}

/// What a reply answers, in a line: who said it and how it begins.
public struct ChannelQuote: Equatable, Sendable {
    public var seq: Int
    public var who: String
    public var text: String
    public init(seq: Int, who: String, text: String) { self.seq = seq; self.who = who; self.text = text }

    /// How a quote begins: the first line, cut at 60 characters.
    public static func snippet(_ text: String) -> String {
        let line = text.split(separator: "\n", omittingEmptySubsequences: true).first.map(String.init) ?? ""
        let one = line.trimmingCharacters(in: .whitespaces)
        return one.count > 60 ? String(one.prefix(59)) + "…" : one
    }
}

/// One bubble: a message's text, its pictures, what it answers; folded into a line when `fold` is set.
public struct ChannelBubble: Identifiable, Equatable, Sendable {
    /// Its number in the channel.
    public var id: Int
    /// Its whole text; empty for pictures alone.
    public var text: String
    /// Ids of its pictures (`loadPicture` fetches the bytes).
    public var images: [String]
    public var quote: ChannelQuote?
    /// Set when the message is folded into one bordered line: the bold start of it ("→ builder", "完成")
    /// and the first sentence. Tapping opens `text` in place.
    public var fold: Fold?

    public struct Fold: Equatable, Sendable {
        public var head: String
        public var summary: String
        public init(head: String, summary: String) { self.head = head; self.summary = summary }
    }

    public init(id: Int, text: String, images: [String] = [], quote: ChannelQuote? = nil, fold: Fold? = nil) {
        self.id = id; self.text = text; self.images = images; self.quote = quote; self.fold = fold
    }
}

/// Messages of one sender in a row: its name and character once, a bubble for each.
public struct ChannelRun: Identifiable, Equatable, Sendable {
    public var id: Int { bubbles.first?.id ?? 0 }
    public var sender: ChannelMember
    public var bubbles: [ChannelBubble]
    public init(sender: ChannelMember, bubbles: [ChannelBubble]) { self.sender = sender; self.bubbles = bubbles }
}

/// What the channel view lists, top to bottom.
public enum ChannelItem: Identifiable, Equatable, Sendable {
    /// A line of time, after a quiet while: "今天 21:05", "21:07".
    case time(id: String, text: String)
    /// What the bus itself says ("Paused: …").
    case note(id: String, text: String)
    /// Agents' messages by one sender.
    case run(ChannelRun)
    /// The person's own message: blue, always whole.
    case mine(ChannelBubble)

    public var id: String {
        switch self {
        case .time(let id, _): "t" + id
        case .note(let id, _): "n" + id
        case .run(let r): "r\(r.id)"
        case .mine(let b): "m\(b.id)"
        }
    }
}

/// The 群信息 panel.
public struct ChannelPanel: Equatable, Sendable {
    public var name: String
    public var topic: String
    /// The team (its roles, lead first), or everyone when the channel has no team.
    public var members: [ChannelMember]
    /// Members the team did not name, working and waiting first; listed under their own heading.
    public var helpers: [ChannelMember]
    /// The channel has a team: the lists get headings ("小组 3", "帮手 4").
    public var sectioned: Bool
    /// How many pictures the channel holds.
    public var pictureCount: Int
    public var paused: Bool
    public init(name: String, topic: String = "", members: [ChannelMember] = [], helpers: [ChannelMember] = [], sectioned: Bool = false,
                pictureCount: Int = 0, paused: Bool = false) {
        self.name = name; self.topic = topic; self.members = members; self.helpers = helpers; self.sectioned = sectioned
        self.pictureCount = pictureCount; self.paused = paused
    }
}

/// Everything the view needs of one channel.
public struct ChannelScreen: Equatable, Sendable {
    public var title: String
    /// The characters of its tile, and who is in the channel.
    public var members: [ChannelMember]
    /// The helpers the team brought in (not in `members`).
    public var helpers: [ChannelMember]
    /// "2 个在工作".
    public var stateLine: String
    public var items: [ChannelItem]
    public var panel: ChannelPanel
    /// The composer's hint: "发消息给 ios".
    public var placeholder: String
    /// The person may not write here (a DM between two agents).
    public var readOnly: Bool

    public init(title: String, members: [ChannelMember], helpers: [ChannelMember] = [], stateLine: String, items: [ChannelItem], panel: ChannelPanel,
                placeholder: String, readOnly: Bool = false) {
        self.title = title; self.members = members; self.helpers = helpers; self.stateLine = stateLine; self.items = items
        self.panel = panel; self.placeholder = placeholder; self.readOnly = readOnly
    }

    /// Changes whenever something is added at the end, also a message appended to the last run (whose
    /// own id stays): the view scrolls on it.
    public var tail: String {
        switch items.last {
        case nil: ""
        case .run(let r)?: "r\(r.id).\(r.bubbles.last?.id ?? 0)"
        case let item?: item.id
        }
    }
}

/// A picture the person is about to send.
public struct ChannelDraftImage: Identifiable, Equatable, Sendable {
    public var id = UUID()
    public var mime: String
    public var data: Data
    public init(mime: String, data: Data) { self.mime = mime; self.data = data }
}

/// What the view asks of its owner. Closures, so the view knows no store.
public struct ChannelActions: Sendable {
    /// Sends text and pictures, as an answer to message `replyTo` when set; true when the Mac took
    /// it (the view then clears the composer), false when it refused or the link is down.
    public var send: @MainActor @Sendable (_ text: String, _ images: [ChannelDraftImage], _ replyTo: Int?) async -> Bool
    /// The person chose to answer message `seq` (the view shows the quote above the composer).
    public var reply: @MainActor @Sendable (_ seq: Int) -> Void
    public var openAgent: @MainActor @Sendable (_ id: String) -> Void
    /// The task board; nil when it cannot be opened from here (the panel then has no row for it).
    public var openBoard: (@MainActor @Sendable () -> Void)?
    /// The pictures of the channel in one place: the view's own gallery when nil.
    public var openPictures: (@MainActor @Sendable () -> Void)?
    /// A paused channel goes on; nil when it cannot be asked.
    public var resume: (@MainActor @Sendable () -> Void)?
    /// A picture's bytes.
    public var loadPicture: @Sendable (_ id: String) async -> Data?
    /// The way back to the list (the iPhone's); nil where there is none.
    public var back: (@MainActor @Sendable () -> Void)?

    public init(send: @escaping @MainActor @Sendable (String, [ChannelDraftImage], Int?) async -> Bool,
                reply: @escaping @MainActor @Sendable (Int) -> Void = { _ in },
                openAgent: @escaping @MainActor @Sendable (String) -> Void = { _ in },
                openBoard: (@MainActor @Sendable () -> Void)? = nil,
                openPictures: (@MainActor @Sendable () -> Void)? = nil,
                resume: (@MainActor @Sendable () -> Void)? = nil,
                loadPicture: @escaping @Sendable (String) async -> Data? = { _ in nil },
                back: (@MainActor @Sendable () -> Void)? = nil) {
        self.send = send; self.reply = reply; self.openAgent = openAgent; self.openBoard = openBoard
        self.openPictures = openPictures; self.resume = resume; self.loadPicture = loadPicture; self.back = back
    }
}

/// How many pictures a message takes, and the biggest, as the server's limits.
public enum ChannelLimits {
    public static let images = 6
    public static let imageBytes = 10 << 20
}
