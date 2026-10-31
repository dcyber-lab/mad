import MadKit
import SwiftUI

/// One agent's conversation: its transcript (/api/chat, polled), the message
/// being written (/api/live, long-polled), and sending to it.
@MainActor
final class ChatModel: ObservableObject {
    let agentID: String
    @Published private(set) var transcript = ChatTranscript()
    @Published private(set) var live = LiveState()
    @Published private(set) var loaded = false
    /// Why the transcript cannot be read now (offline, token refused); nil when it can.
    @Published private(set) var problem: String?
    @Published private(set) var sending = false
    /// A message for the person: the server's reason a send or interrupt failed.
    @Published var notice: String?

    private let client: MadClient
    private let fixture: Bool

    /// `sample` is Debug-only: a fixed conversation instead of a live Mac, and
    /// nothing is sent.
    init(agentID: String, client: MadClient, sample: (page: ChatPage, live: LiveMessage?)? = nil) {
        self.agentID = agentID
        self.client = client
        fixture = sample != nil
        if let sample {
            transcript.apply(sample.page)
            if let m = sample.live { live.receive(m) }
            loaded = true
        }
    }

    /// Reads the transcript every ~2 s and follows the live message, until
    /// the calling task is cancelled.
    func run() async {
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

    private func look() async {
        do {
            let page = try await client.chat(agent: agentID, file: transcript.file, from: transcript.offset)
            transcript.apply(page)
            loaded = true
            problem = nil
        } catch let e as ConnectionError {
            if !Task.isCancelled { problem = e.localizedDescription }
        } catch {}
    }

    private func followLive() async {
        while !Task.isCancelled {
            do {
                // nil is 204: nothing streams, ask again (the server held it for a while).
                if let m = try await client.live(agent: agentID, since: live.version) {
                    live.receive(m)
                }
            } catch {
                if Task.isCancelled { return }
                try? await Task.sleep(for: .seconds(2))
            }
        }
    }

    /// Sends `text`; true when the Mac took it. The agent has to be in window
    /// mode, so it is put there first, as the page does.
    func send(_ text: String, agent: DeckSnapshot.Agent?) async -> Bool {
        let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !t.isEmpty, !sending else { return false }
        if fixture {
            notice = "Sample data: nothing was sent."
            return false
        }
        sending = true
        defer { sending = false }
        do {
            if agent?.inWindowMode != true { try await client.setMode(agent: agentID) }
            try await client.send(agent: agentID, text: t)
            UINotificationFeedbackGenerator().notificationOccurred(.success)
            await look()
            return true
        } catch {
            UINotificationFeedbackGenerator().notificationOccurred(.error)
            notice = error.localizedDescription
            return false
        }
    }

    func interrupt() async {
        if fixture {
            notice = "Sample data: nothing was sent."
            return
        }
        do {
            try await client.interrupt(agent: agentID)
            UINotificationFeedbackGenerator().notificationOccurred(.success)
        } catch {
            notice = error.localizedDescription
        }
    }
}
