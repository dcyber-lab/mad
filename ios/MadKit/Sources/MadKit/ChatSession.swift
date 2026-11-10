import Combine
import Foundation

/// A picture sent with a message.
public struct OutgoingImage: Equatable, Sendable {
    public var mime: String
    public var data: Data
    public init(mime: String, data: Data) { self.mime = mime; self.data = data }
}

/// What a conversation needs of the Mac; `MadClient` is it, a test's fake can be.
public protocol ChatAPI {
    /// What the app keeps of this Mac on the device; nil for a fake.
    var cache: SnapshotCache? { get }
    func chat(agent: String, file: String, from: Int64) async throws -> ChatPage
    func live(agent: String, since: Int64) async throws -> LiveMessage?
    func send(agent: String, text: String) async throws
    func send(agent: String, text: String, images: [OutgoingImage]) async throws
    func setMode(agent: String, mode: String) async throws
    func interrupt(agent: String) async throws
}

extension MadClient: ChatAPI {}

extension ChatAPI {
    public var cache: SnapshotCache? { nil }
    /// A fake that takes text only: the pictures are not sent.
    public func send(agent: String, text: String, images: [OutgoingImage]) async throws {
        try await send(agent: agent, text: text)
    }
}

/// One agent's conversation: its transcript read by looks that never overlap,
/// the message being written, and sending to it.
@MainActor
public final class ChatSession: ObservableObject {
    public let agentID: String
    @Published public private(set) var transcript = ChatTranscript()
    @Published public private(set) var live = LiveState()
    @Published public private(set) var loaded = false
    /// Why the transcript cannot be read now (offline, token refused, no such
    /// agent); nil when it can.
    @Published public private(set) var problem: String?
    @Published public private(set) var sending = false
    /// A message for the person: the server's reason a send or interrupt failed.
    @Published public var notice: String?
    /// What the person sent that the transcript does not show yet: drawn as their bubble at once, not after the
    /// round trips and the agent writing it down.
    @Published public private(set) var outgoing: [Outgoing] = []

    private let api: ChatAPI
    private let fixture: Bool
    private var looking = false
    private var lookAgain = false

    /// `sample` is for a fixed conversation instead of a live Mac: nothing is read or sent.
    public init(agentID: String, api: ChatAPI, sample: (page: ChatPage, live: LiveMessage?)? = nil) {
        self.agentID = agentID
        self.api = api
        fixture = sample != nil
        if let sample {
            transcript.apply(sample.page)
            if let m = sample.live { live.receive(m) }
            loaded = true
        } else if let raw = api.cache?.loadChat(agentID), let page = try? JSONDecoder().decode(ChatPage.self, from: raw),
                  !page.entries.isEmpty {
            // The tail of the conversation as the device kept it: it draws at once, and the looks go on from its offset.
            transcript.apply(page)
            loaded = true
        }
    }

    /// The text the field should hold after `sent` went out: emptied only if
    /// it still is what was sent, so the next message typed meanwhile stays.
    public static func draftAfterSend(sent: String, current: String) -> String {
        current == sent ? "" : current
    }

    #if DEBUG
    /// For tests: moves the cursor as if another look had finished.
    public func applyForTest(_ page: ChatPage) { transcript.apply(page) }
    #endif

    // MARK: Reading

    /// Reads the transcript every ~2 s and follows the live message, until
    /// the calling task is cancelled.
    public func run() async {
        if fixture { return }
        await withTaskGroup(of: Void.self) { group in
            group.addTask { await self.pollTranscript() }
            group.addTask { await self.followLive() }
        }
    }

    private func pollTranscript() async {
        while !Task.isCancelled {
            await look()
            try? await Task.sleep(for: .seconds(2))
        }
    }

    /// One look at a time. A look asked for while another is in flight makes
    /// that one look again when it is done, from where it got to.
    public func look() async {
        if looking {
            lookAgain = true
            return
        }
        looking = true
        defer { looking = false }
        repeat {
            lookAgain = false
            await lookOnce()
        } while lookAgain && !Task.isCancelled
    }

    private func lookOnce() async {
        let (file, offset) = (transcript.file, transcript.offset)
        do {
            let page = try await api.chat(agent: agentID, file: file, from: offset)
            // Only what was read from where the transcript still stands.
            guard transcript.file == file, transcript.offset == offset else { return }
            transcript.apply(page)
            outgoing = Outgoing.pending(outgoing, in: transcript)
            loaded = true
            problem = nil
        } catch {
            if Task.isCancelled { return }
            problem = (error as? ConnectionError)?.localizedDescription ?? error.localizedDescription
        }
    }

    private func followLive() async {
        while !Task.isCancelled {
            do {
                // nil is 204: nothing streams, ask again (the server held it for a while).
                if let m = try await api.live(agent: agentID, since: live.version) {
                    live.receive(m)
                }
            } catch {
                if Task.isCancelled { return }
                try? await Task.sleep(for: .seconds(2))
            }
        }
    }

    // MARK: Writing

    /// Sends `text`; true when the Mac took it. Window mode is asked for
    /// before every send, not trusted from the snapshot: the Mac treats the
    /// same mode as nothing to do, and refuses (with its reason, shown) to
    /// switch a working agent. One send at a time.
    public func send(_ text: String, images: [OutgoingImage] = []) async -> Bool {
        let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !t.isEmpty || !images.isEmpty, !sending else { return false }
        if fixture {
            notice = "这是示例数据：什么也没有发出去。"
            return false
        }
        sending = true
        defer { sending = false }
        let out = Outgoing(text: t, after: Outgoing.userCount(transcript))
        outgoing.append(out)
        do {
            try await api.setMode(agent: agentID, mode: "window")
            if images.isEmpty {
                try await api.send(agent: agentID, text: t)
            } else {
                try await api.send(agent: agentID, text: t, images: images)
            }
        } catch {
            outgoing.removeAll { $0.id == out.id }
            notice = (error as? ConnectionError)?.localizedDescription ?? error.localizedDescription
            return false
        }
        await look()
        return true
    }

    public func interrupt() async {
        if fixture {
            notice = "这是示例数据：什么也没有发出去。"
            return
        }
        do {
            try await api.interrupt(agent: agentID)
        } catch {
            notice = (error as? ConnectionError)?.localizedDescription ?? error.localizedDescription
        }
    }
}

/// A message the person sent, until the transcript has it.
public struct Outgoing: Equatable, Sendable, Identifiable {
    public let id = UUID()
    public let text: String
    /// How many of the person's entries the transcript had when it was sent.
    let after: Int
    let at: Date

    public init(text: String, after: Int, at: Date = Date()) {
        self.text = text
        self.after = after
        self.at = at
    }

    /// Past this it is dropped whatever the transcript says: a bubble never stays for good.
    static let lasts: TimeInterval = 120

    /// The person's entries (a slash command is written as a command).
    static func userCount(_ t: ChatTranscript) -> Int {
        t.entries.filter { $0.kind == "user" || $0.kind == "command" }.count
    }

    /// Those of `list` the transcript does not show yet: one goes once an entry of the person's with its text
    /// comes after where the transcript stood when it was sent (the agent may write it a little changed, so any new
    /// entry of theirs does too after a few seconds), or once it is old.
    public static func pending(_ list: [Outgoing], in t: ChatTranscript, now: Date = Date()) -> [Outgoing] {
        guard !list.isEmpty else { return list }
        let users = t.entries.filter { $0.kind == "user" || $0.kind == "command" }
        return list.filter { o in
            if now.timeIntervalSince(o.at) > lasts { return false }
            let newer = users.count > o.after ? users[o.after...] : []
            if newer.contains(where: { ($0.text ?? "").trimmingCharacters(in: .whitespacesAndNewlines) == o.text }) { return false }
            return newer.isEmpty || now.timeIntervalSince(o.at) < 5
        }
    }
}
