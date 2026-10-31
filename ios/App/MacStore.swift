import Foundation
import MadKit
import Security
import UIKit

/// A failure of the app's own, not the Mac's.
enum StoreError: LocalizedError {
    case keychain(OSStatus)
    case noRandom

    var errorDescription: String? {
        switch self {
        case .keychain(let s): return "The token could not be saved to the Keychain (error \(s)). The Mac was not added."
        case .noRandom: return "The phone could not make a random token."
        }
    }
}

/// The paired Macs: tokens in the Keychain, everything else in UserDefaults.
/// A pairing that was sent but not answered stays as a pending entry (its
/// token in the Keychain, the code in UserDefaults) until it is retried
/// to an answer or refused.
@MainActor
final class MacStore: ObservableObject {
    @Published private(set) var macs: [Mac] = []
    @Published private(set) var pending: [Pending] = []

    struct Pending: Identifiable, Equatable {
        var id: UUID
        var base: URL
        var code: String
        var name: String
        var token: String
        var title: String { Pairing.defaultName(for: base) }
        var pairing: PendingPairing { PendingPairing(base: base, code: code, name: name, token: token) }
    }

    private struct Stored: Codable {
        var id: UUID
        var name: String
        var baseURL: URL
        var deviceID: String?
    }

    private struct StoredPending: Codable {
        var id: UUID
        var base: URL
        var code: String
        var name: String
    }

    private let defaults = UserDefaults.standard
    private let key = "macs"
    private let pendingKey = "pending"

    init() { load() }

    // MARK: Pairing

    /// Adds a Mac from a link: a token link is stored as it is; a pair link
    /// is exchanged for a token this device makes. Throws what the person
    /// should read; a refused or failed save never leaves a half entry.
    func add(_ link: PairingLink) async throws {
        switch link {
        case .token(let mac):
            try add(mac)
        case .pair(let base, let code):
            // The same QR scanned again continues the pairing it started.
            if let p = pending.first(where: { $0.base == base && $0.code == code }) {
                try await complete(p)
                return
            }
            let token = try Self.randomToken()
            let p = Pending(id: UUID(), base: base, code: code, name: UIDevice.current.name, token: token)
            // Saved before it is sent: if the answer is lost, the retry sends the same token.
            try Keychain.set(token, for: Self.pendingAccount(p.id))
            pending.append(p)
            persistPending()
            try await complete(p)
        }
    }

    /// Sends (or re-sends) a pending pairing. On an answer the Mac is stored
    /// and the pending entry goes; when the Mac refuses, the entry goes and
    /// the error says to scan a new QR; on a network error it stays for Retry.
    func complete(_ p: Pending) async throws {
        do {
            let id = try await p.pairing.pair()
            try add(Mac(name: Pairing.defaultName(for: p.base), baseURL: p.base, token: p.token, deviceID: id))
            drop(p)
        } catch ConnectionError.pairingRefused {
            drop(p)
            throw ConnectionError.pairingRefused
        }
    }

    /// Forgets a pending pairing (swipe to delete).
    func drop(_ p: Pending) {
        Keychain.delete(Self.pendingAccount(p.id))
        pending.removeAll { $0.id == p.id }
        persistPending()
    }

    func dropPending(at offsets: IndexSet) {
        for p in offsets.map({ pending[$0] }) { drop(p) }
    }

    // MARK: Macs

    func add(_ mac: Mac) throws {
        // Pairing the same host again replaces the old entry (new token).
        let previous = macs
        var m = mac
        if let i = macs.firstIndex(where: { $0.baseURL == mac.baseURL }) {
            m.id = macs[i].id
            m.name = macs[i].name
            macs[i] = m
        } else {
            macs.append(m)
        }
        do {
            try Keychain.set(m.token, for: m.id.uuidString)
        } catch {
            macs = previous
            throw error
        }
        persist()
    }

    func rename(_ mac: Mac, to name: String) {
        let n = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !n.isEmpty, let i = macs.firstIndex(where: { $0.id == mac.id }) else { return }
        macs[i].name = n
        persist()
    }

    func delete(at offsets: IndexSet) {
        for i in offsets { Keychain.delete(macs[i].id.uuidString) }
        macs.remove(atOffsets: offsets)
        persist()
    }

    // MARK: Storage

    private static func pendingAccount(_ id: UUID) -> String { "pending:" + id.uuidString }

    /// 16 random bytes as 32 lowercase hex digits.
    static func randomToken() throws -> String {
        var bytes = [UInt8](repeating: 0, count: 16)
        guard SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes) == errSecSuccess else {
            throw StoreError.noRandom
        }
        return bytes.map { String(format: "%02x", $0) }.joined()
    }

    private func persist() {
        let list = macs.map { Stored(id: $0.id, name: $0.name, baseURL: $0.baseURL, deviceID: $0.deviceID) }
        if let data = try? JSONEncoder().encode(list) { defaults.set(data, forKey: key) }
    }

    private func persistPending() {
        let list = pending.map { StoredPending(id: $0.id, base: $0.base, code: $0.code, name: $0.name) }
        if let data = try? JSONEncoder().encode(list) { defaults.set(data, forKey: pendingKey) }
    }

    private func load() {
        if let data = defaults.data(forKey: key),
           let list = try? JSONDecoder().decode([Stored].self, from: data) {
            macs = list.compactMap { s in
                guard let t = Keychain.get(s.id.uuidString) else { return nil }
                return Mac(id: s.id, name: s.name, baseURL: s.baseURL, token: t, deviceID: s.deviceID)
            }
        }
        if let data = defaults.data(forKey: pendingKey),
           let list = try? JSONDecoder().decode([StoredPending].self, from: data) {
            pending = list.compactMap { s in
                guard let t = Keychain.get(Self.pendingAccount(s.id)) else { return nil }
                return Pending(id: s.id, base: s.base, code: s.code, name: s.name, token: t)
            }
        }
    }
}

enum Keychain {
    private static let service = "dev.mad.ios.tokens"

    private static func query(_ account: String) -> [String: Any] {
        [kSecClass as String: kSecClassGenericPassword,
         kSecAttrService as String: service,
         kSecAttrAccount as String: account]
    }

    /// Throws when the item could not be written, so a Mac is never kept
    /// without its token.
    static func set(_ value: String, for account: String) throws {
        delete(account)
        var q = query(account)
        q[kSecValueData as String] = Data(value.utf8)
        q[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        let status = SecItemAdd(q as CFDictionary, nil)
        if status != errSecSuccess { throw StoreError.keychain(status) }
    }

    static func get(_ account: String) -> String? {
        var q = query(account)
        q[kSecReturnData as String] = true
        q[kSecMatchLimit as String] = kSecMatchLimitOne
        var out: CFTypeRef?
        guard SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess,
              let d = out as? Data else { return nil }
        return String(data: d, encoding: .utf8)
    }

    static func delete(_ account: String) {
        SecItemDelete(query(account) as CFDictionary)
    }
}
