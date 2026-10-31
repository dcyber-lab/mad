import Foundation

public enum ConnectionError: Error, Equatable, LocalizedError {
    /// 403: the token is not the Mac's current one.
    case refused
    /// The Mac could not be reached at all.
    case offline
    /// Reached, but it did not answer as mad does.
    case unexpected(String)

    public var errorDescription: String? {
        switch self {
        case .refused: return "Token refused, re-pair."
        case .offline: return "Offline: the Mac cannot be reached."
        case .unexpected(let why): return why
        }
    }
}

/// Fetches what a paired Mac shows. The session is injectable for tests.
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
