import Foundation

/// What the apps keep of one Mac on the device, so a screen draws at once from what it saw last and then asks
/// only for what came after: the deck's last look, each channel's last messages, each agent's transcript tail.
///
/// It keeps the Mac's own answers as they came (JSON, in the shape the client reads them), so reading one back is
/// the client's own decoding and nothing of the models is written twice. A channel's file is a `MessagesPage` of
/// its last `messageCap` messages; an agent's is a `ChatPage` of its last `chatCap` entries, from which the next
/// look goes on from the file and offset. Files are in the Application Support directory, in one folder per Mac,
/// so unpairing a Mac or a refused token removes just that Mac's. The token and every other secret stay out: only
/// what the Mac says of its deck and conversations is kept.
///
/// Writes are put off a moment and merged (a poll every second changes a file once, not each time), and `flush()`
/// writes what waits at once.
public final class SnapshotCache: @unchecked Sendable {
    public static let messageCap = 200
    public static let chatCap = 200
    /// How long a change waits to be written.
    public static let writeDelay: TimeInterval = 0.5

    /// The folder the apps keep the caches in: `Application Support/mad/cache`.
    public static var defaultRoot: URL {
        let base = (try? FileManager.default.url(for: .applicationSupportDirectory, in: .userDomainMask, appropriateFor: nil, create: true))
            ?? FileManager.default.temporaryDirectory
        return base.appendingPathComponent("mad", isDirectory: true).appendingPathComponent("cache", isDirectory: true)
    }

    /// The cache of `mac` in the default folder; nil for the fixture server (what it serves changes with its
    /// scenario, so nothing of it is kept).
    public static func standard(for mac: Mac) -> SnapshotCache? {
        if mac.id == Mac.fixtureID { return nil }
        // one object per Mac, whoever asks (a view makes its client again whenever it is drawn again)
        registry.lock.lock(); defer { registry.lock.unlock() }
        if let c = registry.caches[mac.id] { return c }
        let c = SnapshotCache(root: defaultRoot, mac: mac.id.uuidString)
        registry.caches[mac.id] = c
        return c
    }

    /// Writes what waits in every cache now (when the app goes to the background).
    public static func flushAll() {
        registry.lock.lock()
        let all = Array(registry.caches.values)
        registry.lock.unlock()
        all.forEach { $0.flush() }
    }

    private static let registry = Registry()
    private final class Registry: @unchecked Sendable {
        let lock = NSLock()
        var caches: [UUID: SnapshotCache] = [:]
    }

    /// Removes `mac`'s cache (when it is unpaired), without having it open.
    public static func clear(mac: Mac, root: URL = defaultRoot) {
        registry.lock.lock()
        let open = root == defaultRoot ? registry.caches[mac.id] : nil
        registry.lock.unlock()
        open?.clear()
        try? FileManager.default.removeItem(at: root.appendingPathComponent(mac.id.uuidString, isDirectory: true))
    }

    public let directory: URL
    private let lock = NSLock()
    private let queue = DispatchQueue(label: "dev.mad.cache", qos: .utility)
    /// Files changed and not written yet, by name.
    private var waiting: [String: Data] = [:]
    /// What each file holds, by name, once read or merged.
    private var held: [String: [String: Any]] = [:]
    private var writing: DispatchWorkItem?
    private var cleared = false

    public init(root: URL, mac: String) {
        directory = root.appendingPathComponent(mac, isDirectory: true)
    }

    // MARK: The deck

    /// The deck's last look as the Mac answered it; nil when there is none.
    public func loadDeck() -> Data? { read("deck.json") }

    public func saveDeck(_ raw: Data) { enqueue("deck.json", raw) }

    // MARK: Channels

    /// A channel's last messages as a `/api/bus/messages` answer (decode it as `MessagesPage`).
    public func loadChannel(_ id: String) -> Data? { read(Self.channelFile(id)) }

    /// Takes one answer of /api/bus/messages into the channel's file: its messages are added after those held (a
    /// number held is not added twice), the oldest dropped past `messageCap`. An answer whose last number is lower
    /// than the one held means the channel began again: it replaces the file.
    public func mergeChannel(_ id: String, page raw: Data) {
        guard let new = Self.object(raw) else { return }
        let file = Self.channelFile(id)
        lock.lock(); defer { lock.unlock() }
        guard !cleared else { return }
        var state = state(file) ?? [:]
        let heldLast = state["last"] as? Int ?? 0
        let newLast = new["last"] as? Int ?? 0
        var messages = (state["messages"] as? [[String: Any]]) ?? []
        if newLast < heldLast { messages = [] }
        let top = messages.compactMap { $0["seq"] as? Int }.max() ?? 0
        for m in (new["messages"] as? [[String: Any]] ?? []).sorted(by: { ($0["seq"] as? Int ?? 0) < ($1["seq"] as? Int ?? 0) })
        where (m["seq"] as? Int ?? 0) > top {
            messages.append(m)
        }
        if messages.count > Self.messageCap { messages.removeFirst(messages.count - Self.messageCap) }
        state = ["channel": id, "messages": messages, "last": newLast,
                 "paused": new["paused"] as? Bool ?? false, "receipts": new["receipts"] as? [Any] ?? []]
        keep(file, state)
    }

    /// Forgets a channel's file (it began again, or is gone).
    public func forgetChannel(_ id: String) {
        let file = Self.channelFile(id)
        lock.lock(); defer { lock.unlock() }
        held[file] = nil
        waiting[file] = nil
        try? FileManager.default.removeItem(at: directory.appendingPathComponent(file))
    }

    // MARK: Conversations

    /// An agent's transcript tail as a `/api/chat` answer that starts the conversation over (decode it as `ChatPage`).
    public func loadChat(_ agent: String) -> Data? { read(Self.chatFile(agent)) }

    /// Takes one answer of /api/chat into the agent's file: a reset (or another transcript file) replaces the
    /// entries, otherwise they are added after those held, the oldest dropped past `chatCap` (and `earlier` says so).
    public func mergeChat(_ agent: String, page raw: Data) {
        guard let new = Self.object(raw) else { return }
        let file = Self.chatFile(agent)
        lock.lock(); defer { lock.unlock() }
        guard !cleared else { return }
        let old = state(file)
        let reset = (new["reset"] as? Bool ?? false) || old == nil || (old?["file"] as? String) != (new["file"] as? String)
        var entries = reset ? [] : ((old?["entries"] as? [[String: Any]]) ?? [])
        var earlier = reset ? (new["earlier"] as? Bool ?? false) : (old?["earlier"] as? Bool ?? false)
        entries += new["entries"] as? [[String: Any]] ?? []
        if entries.count > Self.chatCap {
            entries.removeFirst(entries.count - Self.chatCap)
            earlier = true
        }
        keep(file, ["file": new["file"] as? String ?? "", "offset": new["offset"] as? Int64 ?? (new["offset"] as? Int).map(Int64.init) ?? 0,
                    "entries": entries, "reset": true, "earlier": earlier])
    }

    // MARK: Writing and removing

    /// Writes what waits, now.
    public func flush() {
        lock.lock()
        writing?.cancel()
        writing = nil
        let batch = waiting
        waiting = [:]
        lock.unlock()
        queue.sync { write(batch) }
    }

    /// Removes everything this Mac has here, and ignores what is still waiting to be written.
    public func clear() {
        lock.lock()
        cleared = true
        writing?.cancel()
        writing = nil
        waiting = [:]
        held = [:]
        lock.unlock()
        queue.sync { try? FileManager.default.removeItem(at: directory) }
    }

    // MARK: Inside

    static func channelFile(_ id: String) -> String { "ch." + safe(id) + ".json" }
    static func chatFile(_ id: String) -> String { "chat." + safe(id) + ".json" }

    /// A name that is one file name whatever the id holds.
    static func safe(_ id: String) -> String {
        id.addingPercentEncoding(withAllowedCharacters: CharacterSet(charactersIn: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_")) ?? "x"
    }

    private static func object(_ raw: Data) -> [String: Any]? {
        (try? JSONSerialization.jsonObject(with: raw)) as? [String: Any]
    }

    /// The file's bytes: what waits to be written, else what the disk has.
    private func read(_ file: String) -> Data? {
        lock.lock()
        if let w = waiting[file] { lock.unlock(); return w }
        let cleared = self.cleared
        lock.unlock()
        if cleared { return nil }
        return try? Data(contentsOf: directory.appendingPathComponent(file))
    }

    /// What a file holds, parsed (from memory, else the disk). Call with the lock held.
    private func state(_ file: String) -> [String: Any]? {
        if let h = held[file] { return h }
        guard let data = waiting[file] ?? (try? Data(contentsOf: directory.appendingPathComponent(file))),
              let o = Self.object(data) else { return nil }
        held[file] = o
        return o
    }

    /// Keeps a file's new state and puts its writing off. Call with the lock held.
    private func keep(_ file: String, _ state: [String: Any]) {
        held[file] = state
        guard let data = try? JSONSerialization.data(withJSONObject: state, options: [.sortedKeys]) else { return }
        waiting[file] = data
        scheduleWrite()
    }

    private func enqueue(_ file: String, _ data: Data) {
        lock.lock(); defer { lock.unlock() }
        guard !cleared else { return }
        waiting[file] = data
        scheduleWrite()
    }

    /// Call with the lock held.
    private func scheduleWrite() {
        guard writing == nil else { return }
        let item = DispatchWorkItem { [weak self] in
            guard let self else { return }
            self.lock.lock()
            let batch = self.waiting
            self.waiting = [:]
            self.writing = nil
            self.lock.unlock()
            self.write(batch)
        }
        writing = item
        queue.asyncAfter(deadline: .now() + Self.writeDelay, execute: item)
    }

    private func write(_ batch: [String: Data]) {
        guard !batch.isEmpty else { return }
        let fm = FileManager.default
        var folder = directory
        do {
            try fm.createDirectory(at: folder, withIntermediateDirectories: true)
            var values = URLResourceValues()
            values.isExcludedFromBackup = true
            try? folder.setResourceValues(values)
            for (name, data) in batch {
                lock.lock(); let gone = cleared; lock.unlock()
                if gone { return }
                try data.write(to: folder.appendingPathComponent(name), options: [.atomic])
            }
        } catch {
            // A cache that cannot be written is only a cache that is not there.
        }
    }
}
