import Combine
import Foundation

/// What a conversation needs of the Mac; `MadClient` is it, a test's fake can be.
public protocol ChatAPI {
    /// What the app keeps of this Mac on the device; nil for a fake.
    var cache: SnapshotCache? { get }
    func chat(agent: String, file: String, from: Int64) async throws -> ChatPage
    func live(agent: String, since: Int64) async throws -> LiveMessage?
    func send(agent: String, text: String) async throws
    func setMode(agent: String, mode: String) async throws
    func interrupt(agent: String) async throws
}

extension MadClient: ChatAPI {}

extension ChatAPI {
    public var cache: SnapshotCache? { nil }
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
    public func send(_ text: String) async -> Bool {
        let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !t.isEmpty, !sending else { return false }
        if fixture {
            notice = "Sample data: nothing was sent."
            return false
        }
        sending = true
        defer { sending = false }
        do {
            try await api.setMode(agent: agentID, mode: "window")
            try await api.send(agent: agentID, text: t)
        } catch {
            notice = (error as? ConnectionError)?.localizedDescription ?? error.localizedDescription
            return false
        }
        await look()
        return true
    }

    public func interrupt() async {
        if fixture {
            notice = "Sample data: nothing was sent."
            return
        }
        do {
            try await api.interrupt(agent: agentID)
        } catch {
            notice = (error as? ConnectionError)?.localizedDescription ?? error.localizedDescription
        }
    }
}
