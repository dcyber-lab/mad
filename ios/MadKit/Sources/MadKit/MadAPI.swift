import Foundation

/// Everything the stores ask of the Mac. `MadClient` is it; a test's fake can be, so the stores'
/// polling and `since` logic run without a server.
public protocol MadAPI: ChatAPI {
    /// GET /api/deck.
    func deck() async throws -> Deck
    /// GET /api/bus/messages?channel&since.
    func messages(channel: String, since: Int) async throws -> MessagesPage
    /// POST /api/bus/post.
    func post(channel: String, text: String, replyTo: Int?, images: [PostImage]) async throws -> PostResult
    /// POST /api/bus/dm.
    func dm(to agent: String, text: String, images: [PostImage]) async throws -> DMResult
    /// POST /api/bus/read.
    func markRead(channel: String, seq: Int) async throws
    /// GET /api/bus/image?id: a picture's bytes.
    func imageData(id: String) async throws -> Data
    /// POST /api/inbox: the person acts on an inbox item.
    func act(key: String, verb: String, option: String?, text: String?) async throws
}

extension MadClient: MadAPI {}
