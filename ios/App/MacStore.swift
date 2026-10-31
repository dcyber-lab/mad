import Foundation
import MadKit
import Security

/// The paired Macs: tokens in the Keychain, everything else in UserDefaults.
@MainActor
final class MacStore: ObservableObject {
    @Published private(set) var macs: [Mac] = []

    private struct Stored: Codable {
        var id: UUID
        var name: String
        var baseURL: URL
    }

    private let defaults = UserDefaults.standard
    private let key = "macs"

    init() { load() }

    func add(_ mac: Mac) {
        // Pairing the same host again replaces the old entry (new token).
        if let i = macs.firstIndex(where: { $0.baseURL == mac.baseURL }) {
            var m = mac
            m.id = macs[i].id
            m.name = macs[i].name
            macs[i] = m
        } else {
            macs.append(mac)
        }
        save(mac: macs.first { $0.baseURL == mac.baseURL }!)
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

    private func save(mac: Mac) {
        Keychain.set(mac.token, for: mac.id.uuidString)
        persist()
    }

    private func persist() {
        let list = macs.map { Stored(id: $0.id, name: $0.name, baseURL: $0.baseURL) }
        if let data = try? JSONEncoder().encode(list) { defaults.set(data, forKey: key) }
    }

    private func load() {
        guard let data = defaults.data(forKey: key),
              let list = try? JSONDecoder().decode([Stored].self, from: data) else { return }
        macs = list.compactMap { s in
            guard let t = Keychain.get(s.id.uuidString) else { return nil }
            return Mac(id: s.id, name: s.name, baseURL: s.baseURL, token: t)
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

    static func set(_ value: String, for account: String) {
        delete(account)
        var q = query(account)
        q[kSecValueData as String] = Data(value.utf8)
        q[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        SecItemAdd(q as CFDictionary, nil)
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
