import Foundation

// The bus: channels and DMs in which the person and the agents talk (internal/bus,
// internal/desktop/bus.go).

/// A picture on a message. Its bytes are fetched by `id` (`MadClient.imageData`).
public struct BusImage: Equatable, Decodable, Identifiable, Sendable {
    /// The SHA-256 of its bytes, in hex.
    public var id: String
    public var mime: String
    public var size: Int
    /// Its size in points, when the server could read it cheaply.
    public var w: Int?
    public var h: Int?
    enum CodingKeys: String, CodingKey { case id, mime, size, w, h }
    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        mime = try c.decodeIfPresent(String.self, forKey: .mime) ?? ""
        size = try c.decodeIfPresent(Int.self, forKey: .size) ?? 0
        w = try c.decodeIfPresent(Int.self, forKey: .w)
        h = try c.decodeIfPresent(Int.self, forKey: .h)
    }
    public init(id: String, mime: String = "image/png", size: Int = 0, w: Int? = nil, h: Int? = nil) {
        self.id = id; self.mime = mime; self.size = size; self.w = w; self.h = h
    }
}

/// One message of a channel.
public struct BusMessage: Equatable, Decodable, Identifiable, Sendable {
    /// Its number in the channel; they only grow.
    public var seq: Int
    public var at: Date?
    public var from: Sender
    /// text, handoff, done, declined, system; others as the server names them.
    public var kind: String
    public var text: String
    /// Who it asks ("you", or an agent's id).
    public var mentions: [String]
    /// The number of the message it answers, 0 for none.
    public var replyTo: Int
    public var handoff: Handoff?
    public var images: [BusImage]
    public var id: Int { seq }

    public struct Sender: Equatable, Decodable, Sendable {
        public var id: String
        public var label: String
        public var kind: String
        enum CodingKeys: String, CodingKey { case id, label, kind }
        public init(from d: Decoder) throws {
            let c = try d.container(keyedBy: CodingKeys.self)
            id = try c.decodeIfPresent(String.self, forKey: .id) ?? ""
            label = try c.decodeIfPresent(String.self, forKey: .label) ?? ""
            kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? ""
        }
        public init(id: String, label: String, kind: String = "") { self.id = id; self.label = label; self.kind = kind }
        /// The person.
        public var isYou: Bool { id == "you" }
    }

    /// The task a handoff, a done or a declined message is about.
    public struct Handoff: Equatable, Decodable, Sendable {
        public var id: String
        public var to: String
        public var status: String
        public var parent: String?
        enum CodingKeys: String, CodingKey { case id, to, status, parent }
        public init(from d: Decoder) throws {
            let c = try d.container(keyedBy: CodingKeys.self)
            id = try c.decodeIfPresent(String.self, forKey: .id) ?? ""
            to = try c.decodeIfPresent(String.self, forKey: .to) ?? ""
            status = try c.decodeIfPresent(String.self, forKey: .status) ?? ""
            parent = try c.decodeIfPresent(String.self, forKey: .parent)
        }
        public init(id: String, to: String = "", status: String = "", parent: String? = nil) {
            self.id = id; self.to = to; self.status = status; self.parent = parent
        }
    }

    enum CodingKeys: String, CodingKey { case seq, at, from, kind, text, mentions, replyTo = "reply_to", handoff, images }

    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        seq = try c.decode(Int.self, forKey: .seq)
        at = MadDate.decode(c, .at)
        from = try c.decodeIfPresent(Sender.self, forKey: .from) ?? Sender(id: "", label: "")
        kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? "text"
        text = try c.decodeIfPresent(String.self, forKey: .text) ?? ""
        mentions = try c.decodeIfPresent([String].self, forKey: .mentions) ?? []
        replyTo = try c.decodeIfPresent(Int.self, forKey: .replyTo) ?? 0
        handoff = try c.decodeIfPresent(Handoff.self, forKey: .handoff)
        images = try c.decodeIfPresent([BusImage].self, forKey: .images) ?? []
    }

    public init(seq: Int, at: Date? = nil, from: Sender, kind: String = "text", text: String = "", mentions: [String] = [],
                replyTo: Int = 0, handoff: Handoff? = nil, images: [BusImage] = []) {
        self.seq = seq; self.at = at; self.from = from; self.kind = kind; self.text = text; self.mentions = mentions
        self.replyTo = replyTo; self.handoff = handoff; self.images = images
    }

    /// It asks the person.
    public var asksYou: Bool { mentions.contains("you") }
}

/// How one of the person's messages stands: the agents it wakes, each with its state
/// (waiting, delivered, read, replied); none when it wakes nobody.
public struct BusReceipt: Equatable, Decodable, Sendable {
    public var seq: Int
    public var targets: [Target]
    public var paused: Bool
    public struct Target: Equatable, Decodable, Sendable {
        public var id: String
        public var label: String
        public var state: String
        public init(id: String, label: String, state: String) { self.id = id; self.label = label; self.state = state }
    }
    enum CodingKeys: String, CodingKey { case seq, targets, paused }
    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        seq = try c.decodeIfPresent(Int.self, forKey: .seq) ?? 0
        targets = try c.decodeIfPresent([Target].self, forKey: .targets) ?? []
        paused = try c.decodeIfPresent(Bool.self, forKey: .paused) ?? false
    }
    public init(seq: Int, targets: [Target] = [], paused: Bool = false) { self.seq = seq; self.targets = targets; self.paused = paused }
}

/// One answer of /api/bus/messages: the messages after the number asked for (at most 200),
/// the channel's last number, whether it is paused, and the receipts of the person's last messages.
public struct MessagesPage: Equatable, Decodable, Sendable {
    public var channel: String
    public var messages: [BusMessage]
    public var last: Int
    public var paused: Bool
    public var receipts: [BusReceipt]
    enum CodingKeys: String, CodingKey { case channel, messages, last, paused, receipts }
    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        channel = try c.decodeIfPresent(String.self, forKey: .channel) ?? ""
        messages = try c.decodeIfPresent([BusMessage].self, forKey: .messages) ?? []
        last = try c.decodeIfPresent(Int.self, forKey: .last) ?? 0
        paused = try c.decodeIfPresent(Bool.self, forKey: .paused) ?? false
        receipts = try c.decodeIfPresent([BusReceipt].self, forKey: .receipts) ?? []
    }
    public init(channel: String, messages: [BusMessage], last: Int, paused: Bool = false, receipts: [BusReceipt] = []) {
        self.channel = channel; self.messages = messages; self.last = last; self.paused = paused; self.receipts = receipts
    }
}

/// What a post comes back with: its number and the labels of the agents it woke.
public struct PostResult: Equatable, Decodable, Sendable {
    public var seq: Int
    public var woke: [String]
    public var warning: String?
    enum CodingKeys: String, CodingKey { case seq, woke, warning }
    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        seq = try c.decodeIfPresent(Int.self, forKey: .seq) ?? 0
        woke = try c.decodeIfPresent([String].self, forKey: .woke) ?? []
        warning = try c.decodeIfPresent(String.self, forKey: .warning)
    }
    public init(seq: Int, woke: [String] = [], warning: String? = nil) { self.seq = seq; self.woke = woke; self.warning = warning }
}

/// What a DM comes back with: the DM's channel, the message's number and who it woke.
public struct DMResult: Equatable, Decodable, Sendable {
    public var channel: String
    public var seq: Int
    public var woke: [String]
    enum CodingKeys: String, CodingKey { case channel, seq, woke }
    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        channel = try c.decodeIfPresent(String.self, forKey: .channel) ?? ""
        seq = try c.decodeIfPresent(Int.self, forKey: .seq) ?? 0
        woke = try c.decodeIfPresent([String].self, forKey: .woke) ?? []
    }
    public init(channel: String, seq: Int, woke: [String] = []) { self.channel = channel; self.seq = seq; self.woke = woke }
}

/// A picture to send: its type and bytes.
public struct PostImage: Equatable, Sendable {
    public var mime: String
    public var data: Data
    public init(mime: String, data: Data) { self.mime = mime; self.data = data }
}

// MARK: The client's bus calls

extension MadClient {
    /// GET /api/bus/messages?channel&since: what the channel says after number `since`.
    public func messagesRequest(channel: String, since: Int) -> URLRequest {
        request("/api/bus/messages", query: [URLQueryItem(name: "channel", value: channel),
                                             URLQueryItem(name: "since", value: String(since))])
    }

    public func messages(channel: String, since: Int = 0) async throws -> MessagesPage {
        let (data, response) = try await runRequest(messagesRequest(channel: channel, since: since))
        try checked { try Self.checkBus(data, response, [200]) }
        let page = try Self.decodeJSON(MessagesPage.self, data)
        cache?.mergeChannel(channel, page: data)
        return page
    }

    /// POST /api/bus/post {channel, text, reply_to, images}: the person says something in a channel
    /// or in a DM of theirs.
    public func postRequest(channel: String, text: String, replyTo: Int?, images: [PostImage]) -> URLRequest {
        var body: [String: Any] = ["channel": channel, "text": text]
        if let replyTo, replyTo > 0 { body["reply_to"] = replyTo }
        if !images.isEmpty { body["images"] = images.map { ["mime": $0.mime, "data": $0.data.base64EncodedString()] } }
        return jsonRequest("/api/bus/post", body)
    }

    public func post(channel: String, text: String, replyTo: Int? = nil, images: [PostImage] = []) async throws -> PostResult {
        let (data, response) = try await runRequest(postRequest(channel: channel, text: text, replyTo: replyTo, images: images))
        try Self.checkBus(data, response, [200])
        return try Self.decodeJSON(PostResult.self, data)
    }

    /// POST /api/bus/dm {to, text, images}: the person starts or goes on with a DM with an agent.
    public func dmRequest(to agent: String, text: String, images: [PostImage]) -> URLRequest {
        var body: [String: Any] = ["to": agent, "text": text]
        if !images.isEmpty { body["images"] = images.map { ["mime": $0.mime, "data": $0.data.base64EncodedString()] } }
        return jsonRequest("/api/bus/dm", body)
    }

    public func dm(to agent: String, text: String, images: [PostImage] = []) async throws -> DMResult {
        let (data, response) = try await runRequest(dmRequest(to: agent, text: text, images: images))
        try Self.checkBus(data, response, [200])
        return try Self.decodeJSON(DMResult.self, data)
    }

    /// POST /api/bus/read {channel, seq}: the person has read the channel up to `seq`.
    public func readRequest(channel: String, seq: Int) -> URLRequest {
        jsonRequest("/api/bus/read", ["channel": channel, "seq": seq])
    }

    public func markRead(channel: String, seq: Int) async throws {
        let (data, response) = try await runRequest(readRequest(channel: channel, seq: seq))
        try Self.checkBus(data, response, [200])
    }

    /// GET /api/bus/image?id: a picture's bytes. The request carries the token, so an image view
    /// that loads by URL has to use this request, not the bare URL.
    public func imageRequest(id: String) -> URLRequest {
        request("/api/bus/image", query: [URLQueryItem(name: "id", value: id)])
    }

    public func imageData(id: String) async throws -> Data {
        let (data, response) = try await runRequest(imageRequest(id: id))
        try Self.checkBus(data, response, [200])
        return data
    }

    /// A path the Mac named, with its query ("/api/image?id=…&file=…", a chat entry's image), as a
    /// request that carries the token. Nil for an address that is not a path on the Mac.
    public func pictureRequest(url: String) -> URLRequest? {
        guard url.hasPrefix("/"), let c = URLComponents(string: url) else { return nil }
        return request(c.path, query: c.queryItems ?? [])
    }

    public func pictureData(url: String) async throws -> Data {
        guard let r = pictureRequest(url: url) else { throw ConnectionError.unexpected("Not an address on the Mac.") }
        let (data, response) = try await runRequest(r)
        try Self.checkBus(data, response, [200])
        return data
    }

    func jsonRequest(_ path: String, _ body: [String: Any]) -> URLRequest {
        var r = request(path)
        r.httpMethod = "POST"
        r.setValue("application/json", forHTTPHeaderField: "Content-Type")
        r.httpBody = try? JSONSerialization.data(withJSONObject: body, options: [.sortedKeys])
        r.timeoutInterval = 30 // a post may carry pictures
        return r
    }

    /// The server's 403 for a bad token reads "forbidden"; any other 403 (a DM that is not the
    /// person's) is its own message, so it is not taken for a refused token.
    static func checkBus(_ data: Data, _ response: URLResponse, _ ok: Set<Int>) throws {
        if (response as? HTTPURLResponse)?.statusCode == 403 {
            let message = String(data: data, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
            if !message.isEmpty && message != "forbidden" { throw ConnectionError.unexpected(message) }
        }
        try check(data, response, ok)
    }
}
