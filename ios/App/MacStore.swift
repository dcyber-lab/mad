import Foundation
import MadKit
import Security
import Combine
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
/// token in the Keychain, the code in UserDefaults). The rules are the
/// PairingBook's (MadKit), which is tested; this keeps the lists on disk.
@MainActor
final class MacStore: ObservableObject {
    let book: PairingBook
    private var forward: AnyCancellable?

    var macs: [Mac] { book.macs }
    var pending: [PairingBook.Pending] { book.pending }

    private struct Stored: Codable {
        var id: UUID
        var name: String
        var baseURL: URL
        var deviceID: String?
        var renamed: Bool?
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
    private let lastKey = "lastMac"
    private var lastMacID: UUID?

    init() {
        let secrets = Keychain()
        #if DEBUG
        // `-resetStore YES` (UI tests): forget every Mac and pending pairing first.
        if UserDefaults.standard.bool(forKey: "resetStore") { Self.forgetEverything(secrets) }
        #endif
        var macs: [Mac] = []
        var pending: [PairingBook.Pending] = []
        if let data = defaults.data(forKey: key),
           let list = try? JSONDecoder().decode([Stored].self, from: data) {
            macs = list.compactMap { s in
                guard let t = secrets.get(s.id.uuidString) else { return nil }
                return Mac(id: s.id, name: s.name, baseURL: s.baseURL, token: t, deviceID: s.deviceID,
                           renamed: s.renamed ?? false)
            }
        }
        if let data = defaults.data(forKey: pendingKey),
           let list = try? JSONDecoder().decode([StoredPending].self, from: data) {
            pending = list.compactMap { s in
                var p = PairingBook.Pending(id: s.id, base: s.base, code: s.code, name: s.name, token: "")
                guard let t = secrets.get(PairingBook.account(p)) else { return nil }
                p.token = t
                return p
            }
        }
        book = PairingBook(secrets: secrets, macs: macs, pending: pending,
                           deviceName: UIDevice.current.name,
                           makeToken: Self.randomToken, send: { try await $0.pair() })
        lastMacID = defaults.string(forKey: lastKey).flatMap(UUID.init)
        book.onChange = { [weak self] in self?.persist() }
        forward = book.objectWillChange.sink { [weak self] in self?.objectWillChange.send() }
    }

    func add(_ link: PairingLink) async throws { try await book.add(link) }
    func complete(_ p: PairingBook.Pending) async throws { try await book.complete(p) }
    func drop(_ p: PairingBook.Pending) { book.drop(p) }
    func rename(_ mac: Mac, to name: String) { book.rename(mac, to: name) }
    #if DEBUG
    private static func forgetEverything(_ secrets: Keychain) {
        let d = UserDefaults.standard
        if let data = d.data(forKey: "macs"), let list = try? JSONDecoder().decode([Stored].self, from: data) {
            for s in list { secrets.delete(s.id.uuidString) }
        }
        if let data = d.data(forKey: "pending"), let list = try? JSONDecoder().decode([StoredPending].self, from: data) {
            for s in list { secrets.delete("pending:" + s.id.uuidString) }
        }
        for k in ["macs", "pending", "lastMac", "expiryDismissedDay"] { d.removeObject(forKey: k) }
    }
    #endif

    /// The Mac last opened: the app opens it again on launch.
    var lastMac: Mac? { macs.first { $0.id == lastMacID } }

    func opened(_ mac: Mac) {
        lastMacID = mac.id
        defaults.set(mac.id.uuidString, forKey: lastKey)
    }

    func adoptMachineName(_ name: String?, for mac: Mac) { book.adoptMachineName(name, for: mac) }

    func dropPending(at offsets: IndexSet) {
        for p in offsets.map({ pending[$0] }) { book.drop(p) }
    }

    func delete(at offsets: IndexSet) {
        for m in offsets.map({ macs[$0] }) {
            if m.id == lastMacID { lastMacID = nil; defaults.removeObject(forKey: lastKey) }
            book.remove(m)
        }
    }

    /// 16 random bytes as 32 lowercase hex digits.
    nonisolated static func randomToken() throws -> String {
        var bytes = [UInt8](repeating: 0, count: 16)
        guard SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes) == errSecSuccess else {
            throw StoreError.noRandom
        }
        return bytes.map { String(format: "%02x", $0) }.joined()
    }

    private func persist() {
        let list = macs.map { Stored(id: $0.id, name: $0.name, baseURL: $0.baseURL, deviceID: $0.deviceID,
                                renamed: $0.renamed) }
        if let data = try? JSONEncoder().encode(list) { defaults.set(data, forKey: key) }
        let waiting = pending.map { StoredPending(id: $0.id, base: $0.base, code: $0.code, name: $0.name) }
        if let data = try? JSONEncoder().encode(waiting) { defaults.set(data, forKey: pendingKey) }
    }
}

struct Keychain: SecretStore {
    private let service = "dev.mad.ios.tokens"

    private func query(_ account: String) -> [String: Any] {
        [kSecClass as String: kSecClassGenericPassword,
         kSecAttrService as String: service,
         kSecAttrAccount as String: account]
    }

    /// Updates the item when it exists and adds it otherwise, so a failed
    /// write leaves the old token as it was.
    func set(_ value: String, for account: String) throws {
        let data = Data(value.utf8)
        var status = SecItemUpdate(query(account) as CFDictionary, [kSecValueData as String: data] as CFDictionary)
        if status == errSecItemNotFound {
            var q = query(account)
            q[kSecValueData as String] = data
            q[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
            status = SecItemAdd(q as CFDictionary, nil)
        }
        if status != errSecSuccess { throw StoreError.keychain(status) }
    }

    func get(_ account: String) -> String? {
        var q = query(account)
        q[kSecReturnData as String] = true
        q[kSecMatchLimit as String] = kSecMatchLimitOne
        var out: CFTypeRef?
        guard SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess,
              let d = out as? Data else { return nil }
        return String(data: d, encoding: .utf8)
    }

    func delete(_ account: String) {
        SecItemDelete(query(account) as CFDictionary)
    }
}
