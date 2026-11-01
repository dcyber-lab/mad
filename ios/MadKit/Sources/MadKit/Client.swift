import Foundation

public enum ConnectionError: Error, Equatable, LocalizedError {
    /// 403: the token is not the Mac's current one.
    case refused
    /// The Mac could not be reached at all.
    case offline
    /// Pairing was refused (400 or 403): the code is bad, used or expired.
    case pairingRefused
    /// 409: the item was answered already or asks something else now; the
    /// message is the server's, for the person.
    case conflict(String)
    /// Reached, but it did not answer as mad does.
    case unexpected(String)

    public var errorDescription: String? {
        switch self {
        case .refused: return "这台 Mac 不认这个令牌了：请重新配对。"
        case .offline: return "连不上这台 Mac：请检查它是否开着、和手机在同一个网络。"
        case .pairingRefused: return "这台 Mac 拒绝了配对：请重新扫描一个新的二维码。"
        case .conflict(let why), .unexpected(let why): return why
        }
    }
}

/// Fetches what a paired Mac shows. The session is injectable for tests.
public struct MadClient: Sendable {
    public var mac: Mac
    public var session: URLSession
    /// What it sees is kept here for the next start (nil: nothing is kept).
    public var cache: SnapshotCache?

    public init(mac: Mac, session: URLSession = .shared, cache: SnapshotCache? = nil) {
        self.mac = mac
        self.session = session
        self.cache = cache
    }

    /// The request for a path. The token travels in the Authorization
    /// header, never in the URL, so it stays out of logs and history.
    public func request(_ path: String, query: [URLQueryItem] = []) -> URLRequest {
        var c = URLComponents(url: mac.baseURL, resolvingAgainstBaseURL: false)!
        c.path = path
        if !query.isEmpty { c.queryItems = query }
        var r = URLRequest(url: c.url!)
        r.setValue("Bearer " + mac.token, forHTTPHeaderField: "Authorization")
        r.timeoutInterval = 10
        return r
    }

    /// POST /api/inbox: the person acts on an inbox item. Sends the item's
    /// key and the act it chose; "open" acts are never sent.
    public func actRequest(key: String, verb: String, option: String?, text: String?) -> URLRequest {
        var r = request("/api/inbox")
        r.httpMethod = "POST"
        r.setValue("application/json", forHTTPHeaderField: "Content-Type")
        var body: [String: String] = ["key": key, "verb": verb]
        if let option, !option.isEmpty { body["option"] = option }
        if let text, !text.isEmpty { body["text"] = text }
        r.httpBody = try? JSONSerialization.data(withJSONObject: body, options: [.sortedKeys])
        return r
    }

    public func act(key: String, verb: String, option: String? = nil, text: String? = nil) async throws {
        let (data, response): (Data, URLResponse)
        do {
            (data, response) = try await session.data(for: actRequest(key: key, verb: verb, option: option, text: text))
        } catch {
            throw ConnectionError.offline
        }
        try Self.checkAct(data, response)
    }

    /// 204 is done; 409 carries the server's message; 403 is a refused token.
    public static func checkAct(_ data: Data, _ response: URLResponse) throws {
        guard let http = response as? HTTPURLResponse else {
            throw ConnectionError.unexpected("收到的不是 HTTP 回应：请检查链接是不是这台 Mac 的。")
        }
        let message = String(data: data, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        switch http.statusCode {
        case 204, 200: return
        case 403: throw ConnectionError.refused
        case 409: throw ConnectionError.conflict(message.isEmpty ? "It is not waiting on you any more." : message)
        default: throw ConnectionError.unexpected(message.isEmpty ? "这台 Mac 返回了 HTTP \(http.statusCode)：稍后再试，或在 Mac 上重启 mad。" : message)
        }
    }

    // MARK: Conversations

    /// GET /api/chat: the first look has no file and from=0; the next ones
    /// pass the `file` and `offset` of the answer before.
    public func chatRequest(agent: String, file: String, from: Int64) -> URLRequest {
        request("/api/chat", query: [URLQueryItem(name: "id", value: agent),
                                     URLQueryItem(name: "file", value: file),
                                     URLQueryItem(name: "from", value: String(from))])
    }

    public func chat(agent: String, file: String = "", from: Int64 = 0) async throws -> ChatPage {
        let (data, response) = try await runRequest(chatRequest(agent: agent, file: file, from: from))
        try checked { try Self.check(data, response, [200]) }
        let page = try Self.decodeJSON(ChatPage.self, data)
        cache?.mergeChat(agent, page: data)
        return page
    }

    /// Runs a check of an answer; a refused token takes what was kept of this Mac with it.
    func checked<T>(_ body: () throws -> T) throws -> T {
        do { return try body() } catch ConnectionError.refused {
            cache?.clear()
            throw ConnectionError.refused
        }
    }

    /// GET /api/live: held until the message changed after `since`.
    public func liveRequest(agent: String, since: Int64) -> URLRequest {
        var r = request("/api/live", query: [URLQueryItem(name: "id", value: agent),
                                             URLQueryItem(name: "since", value: String(since))])
        r.timeoutInterval = 60 // the server holds it open
        return r
    }

    /// The message being written, or nil when nothing streams (204): ask again.
    public func live(agent: String, since: Int64) async throws -> LiveMessage? {
        let (data, response) = try await runRequest(liveRequest(agent: agent, since: since))
        return try Self.decodeLive(data, response)
    }

    public static func decodeLive(_ data: Data, _ response: URLResponse) throws -> LiveMessage? {
        if (response as? HTTPURLResponse)?.statusCode == 204 { return nil }
        try check(data, response, [200])
        return try decodeJSON(LiveMessage.self, data)
    }

    /// POST /api/send?id=<id> {"text"}. The agent must be in window mode.
    public func sendRequest(agent: String, text: String) -> URLRequest {
        var r = request("/api/send", query: [URLQueryItem(name: "id", value: agent)])
        r.httpMethod = "POST"
        r.setValue("application/json", forHTTPHeaderField: "Content-Type")
        r.httpBody = try? JSONSerialization.data(withJSONObject: ["text": text], options: [.sortedKeys])
        return r
    }

    public func send(agent: String, text: String) async throws {
        let (data, response) = try await runRequest(sendRequest(agent: agent, text: text))
        try Self.check(data, response, [204, 200])
    }

    /// POST /api/mode?id=<id>&mode=window: what the page does before it sends.
    public func modeRequest(agent: String, mode: String = "window") -> URLRequest {
        var r = request("/api/mode", query: [URLQueryItem(name: "id", value: agent),
                                             URLQueryItem(name: "mode", value: mode)])
        r.httpMethod = "POST"
        return r
    }

    public func setMode(agent: String, mode: String = "window") async throws {
        let (data, response) = try await runRequest(modeRequest(agent: agent, mode: mode))
        try Self.check(data, response, [204, 200])
    }

    /// POST /api/interrupt?id=<id>, no body: stops what it does now.
    public func interruptRequest(agent: String) -> URLRequest {
        var r = request("/api/interrupt", query: [URLQueryItem(name: "id", value: agent)])
        r.httpMethod = "POST"
        return r
    }

    public func interrupt(agent: String) async throws {
        let (data, response) = try await runRequest(interruptRequest(agent: agent))
        try Self.check(data, response, [204, 200])
    }

    /// Sends a request; any failure to get an answer is .offline.
    func runRequest(_ r: URLRequest) async throws -> (Data, URLResponse) {
        do { return try await session.data(for: r) } catch { throw ConnectionError.offline }
    }

    /// Maps a status to the error the person should read; `ok` are the ones that pass.
    /// 403 is a refused token, 409 carries the server's message.
    public static func check(_ data: Data, _ response: URLResponse, _ ok: Set<Int>) throws {
        guard let http = response as? HTTPURLResponse else {
            throw ConnectionError.unexpected("收到的不是 HTTP 回应：请检查链接是不是这台 Mac 的。")
        }
        if ok.contains(http.statusCode) { return }
        let message = String(data: data, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        switch http.statusCode {
        case 403: throw ConnectionError.refused
        case 409: throw ConnectionError.conflict(message.isEmpty ? "The Mac could not do that now." : message)
        case 404: throw ConnectionError.unexpected(message.isEmpty ? "No such agent." : message)
        default: throw ConnectionError.unexpected(message.isEmpty ? "这台 Mac 返回了 HTTP \(http.statusCode)：稍后再试，或在 Mac 上重启 mad。" : message)
        }
    }

    static func decodeJSON<T: Decodable>(_ type: T.Type, _ data: Data) throws -> T {
        do { return try JSONDecoder().decode(type, from: data) } catch {
            throw ConnectionError.unexpected("这不像是 mad 的回应：请检查链接是不是 mad 的。")
        }
    }

    public func deck() async throws -> Deck {
        let (data, response): (Data, URLResponse)
        do {
            (data, response) = try await session.data(for: request("/api/deck"))
        } catch {
            throw ConnectionError.offline
        }
        let deck = try checked { try Self.decode(data, response) }
        cache?.saveDeck(data)
        return deck
    }

    /// The deck a cache holds, for the first look.
    public static func cachedDeck(_ cache: SnapshotCache?) -> Deck? {
        cache?.loadDeck().flatMap { try? JSONDecoder().decode(Deck.self, from: $0) }
    }

    /// Maps a response to a snapshot or the error the person should see.
    public static func decode(_ data: Data, _ response: URLResponse) throws -> Deck {
        guard let http = response as? HTTPURLResponse else {
            throw ConnectionError.unexpected("收到的不是 HTTP 回应：请检查链接是不是这台 Mac 的。")
        }
        switch http.statusCode {
        case 200: break
        case 403: throw ConnectionError.refused
        default: throw ConnectionError.unexpected("这台 Mac 返回了 HTTP \(http.statusCode)：稍后再试，或在 Mac 上重启 mad。")
        }
        do {
            return try JSONDecoder().decode(Deck.self, from: data)
        } catch {
            throw ConnectionError.unexpected("这不像是 mad 的回应：请检查链接是不是 mad 的。")
        }
    }
}

/// The Mac's answer to a pairing: this device's id, and the Mac's friendly
/// name (nil from a server that does not send it).
public struct PairResult: Equatable, Sendable {
    public var id: String
    public var machine: String?
    public init(id: String, machine: String? = nil) { self.id = id; self.machine = machine }
}

/// A pairing that has been started: the code from the QR, the token this
/// device made for it, and the name it gives itself. A retry after a lost
/// answer sends the same request again, so the Mac answers with the same id.
public struct PendingPairing: Equatable, Sendable {
    public var base: URL
    public var code: String
    public var name: String
    public var token: String

    public init(base: URL, code: String, name: String, token: String) {
        self.base = base; self.code = code; self.name = name; self.token = token
    }

    /// POST <base>/api/pair {"code","name","token"}. No Authorization: the
    /// device has no token the Mac knows yet.
    public var request: URLRequest {
        var c = URLComponents(url: base, resolvingAgainstBaseURL: false)!
        c.path = "/api/pair"
        var r = URLRequest(url: c.url!)
        r.httpMethod = "POST"
        r.timeoutInterval = 10
        r.setValue("application/json", forHTTPHeaderField: "Content-Type")
        r.httpBody = try? JSONSerialization.data(
            withJSONObject: ["code": code, "name": name, "token": token], options: [.sortedKeys])
        return r
    }

    /// Exchanges the code for this device's id. Network errors are .offline
    /// (keep the pending pairing and retry); 400 and 403 are .pairingRefused.
    public func pair(session: URLSession = .shared) async throws -> PairResult {
        let (data, response): (Data, URLResponse)
        do {
            (data, response) = try await session.data(for: request)
        } catch {
            throw ConnectionError.offline
        }
        return try Self.check(data, response)
    }

    public static func check(_ data: Data, _ response: URLResponse) throws -> PairResult {
        guard let http = response as? HTTPURLResponse else {
            throw ConnectionError.unexpected("收到的不是 HTTP 回应：请检查链接是不是这台 Mac 的。")
        }
        switch http.statusCode {
        case 200:
            struct Reply: Decodable { var id: String; var machine: String? }
            guard let r = try? JSONDecoder().decode(Reply.self, from: data), !r.id.isEmpty else {
                throw ConnectionError.unexpected("这不像是 mad 的回应：请检查链接是不是 mad 的。")
            }
            let m = r.machine?.trimmingCharacters(in: .whitespacesAndNewlines)
            return PairResult(id: r.id, machine: (m?.isEmpty ?? true) ? nil : m)
        case 400, 403:
            throw ConnectionError.pairingRefused
        default:
            throw ConnectionError.unexpected("这台 Mac 返回了 HTTP \(http.statusCode)：稍后再试，或在 Mac 上重启 mad。")
        }
    }
}
