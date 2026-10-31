import Foundation
import MadKit
import MadUI

/// A channel's messages and its deck onto what the channel view draws, as the page's channel view draws
/// it (internal/desktop/web/bus.js): runs of one sender's messages, a line of time after a quiet while,
/// long messages and agents' reports folded to a line (unless they are the person's or ask the person),
/// the members with how they stand.
public enum ChannelMapping {
    /// A quiet this long (seconds) between two messages gets a line of time.
    public static let quiet: TimeInterval = 600
    /// An agent's message longer than this many lines, or characters, is folded.
    public static let foldLines = 3
    public static let foldChars = 280

    public static func screen(channel id: String, messages: [BusMessage], deck: Deck?, paused: Bool = false,
                              calendar: Calendar = .current, now: Date = Date()) -> ChannelScreen {
        let channel = deck?.channel(id: id)
        let title = Self.title(id: id, channel: channel)
        let members = Self.members(channel: channel, deck: deck)
        let sorted = messages.sorted { $0.seq < $1.seq }
        let items = Self.items(sorted, channel: channel, deck: deck, calendar: calendar, now: now)
        let pictures = sorted.reduce(0) { $0 + $1.images.count }
        let working = members.filter { $0.state == .working }.count
        let needing = members.filter { $0.state == .needsYou }.count
        let line = members.isEmpty ? "" : working > 0 ? "\(working) 个在工作" : needing > 0 ? "\(needing) 个在等你" : "\(members.count) 位成员"
        let isPaused = paused || (channel?.paused ?? false)
        return ChannelScreen(
            title: title, members: members, stateLine: line, items: items,
            panel: ChannelPanel(name: title, topic: channel?.topic ?? "", members: members, pictureCount: pictures, paused: isPaused),
            placeholder: "发消息给 \(title)", readOnly: (channel?.dm ?? false) && !(channel?.yours ?? false))
    }

    static func title(id: String, channel: Channel?) -> String {
        guard let c = channel, c.dm else { return id }
        return c.title.hasPrefix("@") || c.title.hasPrefix("#") ? String(c.title.dropFirst()) : c.title
    }

    // MARK: members

    static func members(channel: Channel?, deck: Deck?) -> [ChannelMember] {
        guard let c = channel else { return [] }
        let leadID = c.lead ?? c.team?.roles.first(where: \.lead)?.agent
        return c.members.map { m in
            let agent = deck?.agent(id: m.id)
            let state = agent.map { CharacterState(status: $0.state) } ?? .idle
            return ChannelMember(id: m.id, kind: agent?.kind ?? m.kind, state: state, name: m.label,
                                 status: statusText(agent, state), isLead: m.id == leadID)
        }
    }

    /// How an agent stands, in a few words: "在工作 · Bash go test", "在等你", "空闲", "已停止".
    static func statusText(_ a: Agent?, _ state: CharacterState) -> String {
        switch state {
        case .working:
            let doing = [a?.tool, a?.target].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " ")
            return doing.isEmpty ? "在工作" : "在工作 · \(doing)"
        case .needsYou: return "在等你"
        case .starting: return "在启动"
        case .stopped: return "已停止"
        case .idle: return "空闲"
        }
    }

    // MARK: items

    static func items(_ messages: [BusMessage], channel: Channel?, deck: Deck?, calendar: Calendar, now: Date) -> [ChannelItem] {
        var out: [ChannelItem] = []
        var previous: BusMessage?
        var openRun = false // the last item is a run a message may join
        for m in messages {
            if m.kind == "system" {
                out.append(.note(id: String(m.seq), text: m.text))
                openRun = false
                previous = m
                continue
            }
            if let label = timeLine(for: m, after: previous, calendar: calendar, now: now) {
                out.append(.time(id: String(m.seq), text: label))
                openRun = false
            }
            let bubble = Self.bubble(m, messages: messages, channel: channel, deck: deck)
            if m.from.isYou {
                out.append(.mine(bubble))
                openRun = false
            } else if openRun, case .run(var run) = out.last, run.sender.id == m.from.id {
                run.bubbles.append(bubble)
                out[out.count - 1] = .run(run)
            } else {
                out.append(.run(ChannelRun(sender: sender(m, deck: deck), bubbles: [bubble])))
                openRun = true
            }
            previous = m
        }
        return out
    }

    static func sender(_ m: BusMessage, deck: Deck?) -> ChannelMember {
        let agent = deck?.agent(id: m.from.id)
        return ChannelMember(id: m.from.id, kind: agent?.kind ?? m.from.kind,
                             state: agent.map { CharacterState(status: $0.state) } ?? .idle, name: m.from.label)
    }

    /// A line of time before `m`: the first message, one after a quiet of ten minutes or more, one on
    /// another day. "今天 21:05" the first time on a day, "21:07" after.
    static func timeLine(for m: BusMessage, after prev: BusMessage?, calendar: Calendar, now: Date) -> String? {
        guard let at = m.at else { return nil }
        let before = prev?.at
        let newDay = before.map { !calendar.isDate($0, inSameDayAs: at) } ?? true
        guard newDay || at.timeIntervalSince(before ?? at) >= quiet else { return nil }
        let hm = String(format: "%02d:%02d", calendar.component(.hour, from: at), calendar.component(.minute, from: at))
        if !newDay { return hm }
        if calendar.isDate(at, inSameDayAs: now) { return "今天 \(hm)" }
        if let y = calendar.date(byAdding: .day, value: -1, to: now), calendar.isDate(at, inSameDayAs: y) { return "昨天 \(hm)" }
        return "\(calendar.component(.month, from: at))月\(calendar.component(.day, from: at))日 \(hm)"
    }

    // MARK: a bubble

    static func bubble(_ m: BusMessage, messages: [BusMessage], channel: Channel?, deck: Deck?) -> ChannelBubble {
        ChannelBubble(id: m.seq, text: m.text, images: m.images.map(\.id), quote: quote(of: m, in: messages),
                      fold: fold(m, channel: channel, deck: deck))
    }

    static func quote(of m: BusMessage, in messages: [BusMessage]) -> ChannelQuote? {
        guard m.replyTo > 0 else { return nil }
        guard let parent = messages.first(where: { $0.seq == m.replyTo }) else {
            return ChannelQuote(seq: m.replyTo, who: "#\(m.replyTo)", text: "")
        }
        let text = parent.text.isEmpty && !parent.images.isEmpty ? "[图片]" : ChannelQuote.snippet(plain(parent.text))
        return ChannelQuote(seq: parent.seq, who: parent.from.isYou ? "你" : parent.from.label, text: text)
    }

    /// The person is asked: `@you` in the text (the bus does not list the person among the mentions).
    static func asksYou(_ m: BusMessage) -> Bool {
        if m.mentions.contains("you") { return true }
        return m.text.range(of: "(^|[\\s(（])@you(?![\\p{L}\\p{N}_-])", options: [.regularExpression, .caseInsensitive]) != nil
    }

    static func isReport(_ m: BusMessage) -> Bool {
        ["handoff", "done", "declined"].contains(m.kind)
    }

    /// The fold of an agent's message, nil when it is shown whole: the person's, mad's own, any that asks
    /// the person, and whatever an agent says in a DM with the person (all of it is addressed to them).
    static func fold(_ m: BusMessage, channel: Channel?, deck: Deck?) -> ChannelBubble.Fold? {
        if m.from.isYou || m.from.id == "mad" || m.kind == "system" { return nil }
        if let c = channel, c.dm, c.yours { return nil }
        if asksYou(m) { return nil }
        let lines = m.text.split(separator: "\n", omittingEmptySubsequences: true).filter { !$0.trimmingCharacters(in: .whitespaces).isEmpty }
        guard isReport(m) || lines.count > foldLines || m.text.count > foldChars else { return nil }
        let declined = m.kind == "declined" || m.handoff?.status == "declined"
        var first = firstSentence(String(lines.first ?? ""))
        let head: String
        switch m.kind {
        case "handoff":
            let to = m.handoff?.to ?? ""
            head = "→ " + (deck?.agent(id: to)?.label ?? to)
        case "done", "declined":
            head = declined ? "拒绝" : "完成"
            first = first.replacingOccurrences(of: "^(done|declined|完成|拒绝)[:：]\\s*", with: "", options: [.regularExpression, .caseInsensitive])
        default: head = ""
        }
        return ChannelBubble.Fold(head: head, summary: first)
    }

    // MARK: text

    /// A line with its markdown taken off.
    static func plain(_ s: String) -> String {
        var t = s.trimmingCharacters(in: .whitespacesAndNewlines)
        t = t.replacingOccurrences(of: "^[#>\\-*\\s]+", with: "", options: .regularExpression)
        // a link is its text; a PR's address is its number, any other its host
        t = t.replacingOccurrences(of: "\\[([^\\]]*)\\]\\([^)]*\\)", with: "$1", options: .regularExpression)
        t = t.replacingOccurrences(of: "PR\\s+https?://\\S+/pull/(\\d+)\\S*", with: "PR #$1", options: .regularExpression)
        t = t.replacingOccurrences(of: "https?://\\S+?/pull/(\\d+)\\S*", with: "PR #$1", options: .regularExpression)
        t = t.replacingOccurrences(of: "https?://([^\\s/,;)]+)\\S*", with: "$1", options: .regularExpression)
        t = t.replacingOccurrences(of: "[*_`]", with: "", options: .regularExpression)
        return t.trimmingCharacters(in: .whitespaces)
    }

    /// The first sentence of a line (its end is a CJK full stop, or a dot, ! or ? followed by a space or the
/// end), cut at 120 characters.
    static func firstSentence(_ line: String) -> String {
        let p = plain(line)
        var one = p
        if let r = p.range(of: "[。！？]|[.!?](\\s|$)", options: .regularExpression), r.lowerBound > p.startIndex {
            one = String(p[p.startIndex..<r.upperBound]).trimmingCharacters(in: .whitespaces)
        }
        return one.count > 120 ? String(one.prefix(119)) + "…" : one
    }
}
