import Foundation

/// How the Mac stands as to serving itself to a phone (GET /api/phone).
public struct PhoneStatus: Equatable, Decodable, Sendable {
    public var on: Bool
    /// "tailscale" or "cloudflare".
    public var via: String?
    public var host: String?
    /// What the phone opens, token and all.
    public var url: String?
    /// Why it cannot be on: no Tailscale, HTTPS off, no cloudflared.
    public var problem: String?
}

/// A paired phone (GET /api/phone/devices).
public struct PhoneDevice: Equatable, Decodable, Identifiable, Sendable {
    public var id: String
    public var name: String
    public var lastSeen: Date?

    enum CodingKeys: String, CodingKey { case id, name, lastSeen = "last_seen" }
}

/// What the Mac's phone dialog needs: the status, putting it on and off, the QR code, the paired phones.
public protocol PhoneAPI: Sendable {
    func phoneStatus() async throws -> PhoneStatus
    /// POST (serve to the phone) or DELETE (stop) /api/phone; the status after.
    func phoneSet(on: Bool) async throws -> PhoneStatus
    func phoneQR() async throws -> Data
    func phoneDevices() async throws -> [PhoneDevice]
    func phoneRemove(id: String) async throws
}

extension MadClient: PhoneAPI {
    private func phoneRequest(_ path: String, method: String, query: [URLQueryItem] = [], timeout: TimeInterval = 10) -> URLRequest {
        var r = request(path, query: query)
        r.httpMethod = method
        r.timeoutInterval = timeout
        return r
    }

    private func phoneJSON<T: Decodable>(_ type: T.Type, _ r: URLRequest) async throws -> T {
        let (data, response) = try await runRequest(r)
        try Self.checkBus(data, response, [200])
        let d = JSONDecoder()
        d.dateDecodingStrategy = .custom { dec in
            let s = try dec.singleValueContainer().decode(String.self)
            let f = ISO8601DateFormatter()
            f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            if let date = f.date(from: s) { return date }
            f.formatOptions = [.withInternetDateTime]
            if let date = f.date(from: s) { return date }
            throw DecodingError.dataCorrupted(.init(codingPath: dec.codingPath, debugDescription: "date"))
        }
        return try d.decode(T.self, from: data)
    }

    public func phoneStatus() async throws -> PhoneStatus {
        try await phoneJSON(PhoneStatus.self, phoneRequest("/api/phone", method: "GET"))
    }

    public func phoneSet(on: Bool) async throws -> PhoneStatus {
        // turning it on can take a while (tailscale serve)
        try await phoneJSON(PhoneStatus.self, phoneRequest("/api/phone", method: on ? "POST" : "DELETE", timeout: 40))
    }

    public func phoneQR() async throws -> Data {
        let (data, response) = try await runRequest(phoneRequest("/api/phone/qr", method: "GET",
                                                                  query: [URLQueryItem(name: "t", value: String(Int(Date().timeIntervalSince1970)))]))
        try Self.checkBus(data, response, [200])
        return data
    }

    public func phoneDevices() async throws -> [PhoneDevice] {
        try await phoneJSON([PhoneDevice].self, phoneRequest("/api/phone/devices", method: "GET"))
    }

    public func phoneRemove(id: String) async throws {
        let (data, response) = try await runRequest(phoneRequest("/api/phone/devices", method: "DELETE", query: [URLQueryItem(name: "id", value: id)]))
        try Self.checkBus(data, response, [200])
    }
}
