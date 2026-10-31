import Combine
import Foundation

/// Where tokens are kept (the Keychain in the app). `set` must leave the old
/// value intact when it fails, so a failed re-pair never costs the working token.
public protocol SecretStore {
    func set(_ value: String, for account: String) throws
    func get(_ account: String) -> String?
    func delete(_ account: String)
}

/// The paired Macs and the pairings still waiting for an answer, with the
/// rules that keep them honest: a pairing's token is saved before it is sent,
/// a lost answer keeps it for a retry, a refusal or a delete drops it, a late
/// answer for a deleted pairing installs nothing, and a failed save never
/// loses the old token. The app persists the lists through `onChange`.
@MainActor
public final class PairingBook: ObservableObject {
    public struct Pending: Identifiable, Equatable, Sendable {
        public var id: UUID
        public var base: URL
        public var code: String
        public var name: String
        public var token: String
        public var title: String { Pairing.defaultName(for: base) }
        public var pairing: PendingPairing { PendingPairing(base: base, code: code, name: name, token: token) }

        public init(id: UUID = UUID(), base: URL, code: String, name: String, token: String) {
            self.id = id; self.base = base; self.code = code; self.name = name; self.token = token
        }
    }

    @Published public private(set) var macs: [Mac]
    @Published public private(set) var pending: [Pending]
    /// Called after every change to either list.
    public var onChange: () -> Void = {}

    private let secrets: SecretStore
    private let send: (PendingPairing) async throws -> PairResult
    private let makeToken: () throws -> String
    private let deviceName: String
    private var inFlight: Set<UUID> = []

    public init(secrets: SecretStore, macs: [Mac] = [], pending: [Pending] = [], deviceName: String,
                makeToken: @escaping () throws -> String,
                send: @escaping (PendingPairing) async throws -> PairResult) {
        self.secrets = secrets
        self.macs = macs
        self.pending = pending
        self.deviceName = deviceName
        self.makeToken = makeToken
        self.send = send
    }

    public static func account(_ pending: Pending) -> String { "pending:" + pending.id.uuidString }

    // MARK: Pairing

    /// Adds a Mac from a link: a token link is stored as it is; a pair link
    /// is exchanged for a token this device makes.
    public func add(_ link: PairingLink) async throws {
        switch link {
        case .token(let mac):
            try add(mac)
        case .pair(let base, let code):
            // The same QR scanned again continues the pairing it started.
            if let p = pending.first(where: { $0.base == base && $0.code == code }) {
                try await complete(p)
                return
            }
            let p = Pending(base: base, code: code, name: deviceName, token: try makeToken())
            // Saved before it is sent: if the answer is lost, the retry sends the same token.
            try secrets.set(p.token, for: Self.account(p))
            pending.append(p)
            onChange()
            try await complete(p)
        }
    }

    /// Sends (or re-sends) a pending pairing. On an answer the Mac is stored
    /// and the entry goes, unless the entry was deleted meanwhile, in which
    /// case the answer is dropped. A refusal drops the entry and throws; a
    /// network error keeps it for a retry.
    public func complete(_ p: Pending) async throws {
        guard inFlight.insert(p.id).inserted else { return }
        defer { inFlight.remove(p.id) }
        do {
            let result = try await send(p.pairing)
            guard pending.contains(where: { $0.id == p.id && $0.code == p.code }) else { return }
            try add(Mac(name: result.machine ?? Pairing.defaultName(for: p.base), baseURL: p.base,
                        token: p.token, deviceID: result.id))
            drop(p)
        } catch ConnectionError.pairingRefused {
            drop(p)
            throw ConnectionError.pairingRefused
        }
    }

    /// Forgets a pending pairing.
    public func drop(_ p: Pending) {
        guard pending.contains(where: { $0.id == p.id }) else { return }
        secrets.delete(Self.account(p))
        pending.removeAll { $0.id == p.id }
        onChange()
    }

    // MARK: Macs

    /// Adds a Mac, or replaces the one at the same host (a new token). The
    /// token is saved first: when that fails, nothing changes.
    public func add(_ mac: Mac) throws {
        var m = mac
        let i = macs.firstIndex { $0.baseURL == mac.baseURL }
        if let i {
            m.id = macs[i].id
            m.name = macs[i].name
            m.renamed = macs[i].renamed
        }
        try secrets.set(m.token, for: m.id.uuidString)
        if let i { macs[i] = m } else { macs.append(m) }
        onChange()
    }

    public func rename(_ mac: Mac, to name: String) {
        let n = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !n.isEmpty, let i = macs.firstIndex(where: { $0.id == mac.id }) else { return }
        macs[i].name = n
        macs[i].renamed = true
        onChange()
    }

    /// The Mac's own name, from its snapshot: it replaces the name unless the
    /// person renamed the Mac.
    public func adoptMachineName(_ name: String?, for mac: Mac) {
        guard let name, !name.isEmpty, let i = macs.firstIndex(where: { $0.id == mac.id }),
              !macs[i].renamed, macs[i].name != name else { return }
        macs[i].name = name
        onChange()
    }

    public func remove(_ mac: Mac) {
        guard let i = macs.firstIndex(where: { $0.id == mac.id }) else { return }
        secrets.delete(mac.id.uuidString)
        macs.remove(at: i)
        onChange()
    }
}
