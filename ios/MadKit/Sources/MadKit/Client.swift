import Foundation

public enum ConnectionError: Error, Equatable, LocalizedError {
    /// 403: the token is not the Mac's current one.
    case refused
    /// The Mac could not be reached at all.
    case offline
    /// Pairing was refused (400 or 403): the code is bad, used or expired.
    case pairingRefused
    /// Reached, but it did not answer as mad does.
    case unexpected(String)

    public var errorDescription: String? {
        switch self {
        case .refused: return "Token refused, re-pair."
        case .offline: return "Offline: the Mac cannot be reached."
        case .pairingRefused: return "The Mac refused to pair: scan a new QR code."
        case .unexpected(let why): return why
        }
    }
}

/// Asks a paired Mac a question. The session is injectable for tests.
public struct MadClient: Sendable {
    public var mac: Mac
    public var session: URLSession

    public init(mac: Mac, session: URLSession = .shared) {
        self.mac = mac
        self.session = session
    }

    /// The request for a path. The token travels in the Authorization
    /// header, never in the URL, so it stays out of logs and history.
    public func request(_ path: String) -> URLRequest {
        var c = URLComponents(url: mac.baseURL, resolvingAgainstBaseURL: false)!
        c.path = path
        var r = URLRequest(url: c.url!)
        r.setValue("Bearer " + mac.token, forHTTPHeaderField: "Authorization")
        r.timeoutInterval = 10
        return r
    }

    /// GET /api/deck: the little of it the app reads (the Mac's name).
    public func deck() async throws -> DeckSnapshot {
        let (data, response): (Data, URLResponse)
        do {
            (data, response) = try await session.data(for: request("/api/deck"))
        } catch {
            throw ConnectionError.offline
        }
        return try Self.decode(data, response)
    }

    /// Maps a response to a snapshot or the error the person should see.
    public static func decode(_ data: Data, _ response: URLResponse) throws -> DeckSnapshot {
        guard let http = response as? HTTPURLResponse else {
            throw ConnectionError.unexpected("Not an HTTP answer.")
        }
        switch http.statusCode {
        case 200: break
        case 403: throw ConnectionError.refused
        default: throw ConnectionError.unexpected("The Mac answered HTTP \(http.statusCode).")
        }
        do {
            return try JSONDecoder().decode(DeckSnapshot.self, from: data)
        } catch {
            throw ConnectionError.unexpected("This does not look like mad's answer.")
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
            throw ConnectionError.unexpected("Not an HTTP answer.")
        }
        switch http.statusCode {
        case 200:
            struct Reply: Decodable { var id: String; var machine: String? }
            guard let r = try? JSONDecoder().decode(Reply.self, from: data), !r.id.isEmpty else {
                throw ConnectionError.unexpected("This does not look like mad's answer.")
            }
            let m = r.machine?.trimmingCharacters(in: .whitespacesAndNewlines)
            return PairResult(id: r.id, machine: (m?.isEmpty ?? true) ? nil : m)
        case 400, 403:
            throw ConnectionError.pairingRefused
        default:
            throw ConnectionError.unexpected("The Mac answered HTTP \(http.statusCode).")
        }
    }
}
