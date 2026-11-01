import CryptoKit
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
        let open = root == defaultRoot ? registry.caches.removeValue(forKey: mac.id) : nil
        registry.lock.unlock()
        open?.retire()
        try? FileManager.default.removeItem(at: root.appendingPathComponent(mac.id.uuidString, isDirectory: true))
    }

    public let directory: URL
    /// The most files of channels and agents kept for one Mac, and the most bytes in them: past either, the ones
    /// used longest ago go.
    public let maxFiles: Int
    public let maxBytes: Int
    /// The least time between two writes of the deck's look (it changes on every poll, for the time it carries).
    public let deckInterval: TimeInterval

    private let lock = NSLock()
    private let queue = DispatchQueue(label: "dev.mad.cache", qos: .utility)
    /// Files changed and not written yet, by name.
    private var waiting: [String: Data] = [:]
    /// What each file holds, by name, once read or merged.
    private var held: [String: [String: Any]] = [:]
    /// The deck's latest look, until it is let through to be written.
    private var deckPending: Data?
    private var lastDeckWrite = Date.distantPast
    /// A digest of what each file last had written: a write of the same bytes is skipped.
    private var digests: [String: Data] = [:]
    private var writing: DispatchWorkItem?
    /// Counted up by `clear()`: a write that was begun before is dropped.
    private var generation = 0
    /// The Mac is unpaired: nothing is kept, ever (the registry makes a new object if the id comes back).
    private var retired = false
    private var pruned = false

    public init(root: URL, mac: String, maxFiles: Int = 50, maxBytes: Int = 20 << 20, deckInterval: TimeInterval = 45) {
        directory = root.appendingPathComponent(mac, isDirectory: true)
        self.maxFiles = maxFiles
        self.maxBytes = maxBytes
        self.deckInterval = deckInterval
    }

    // MARK: The deck

    /// The deck's last look as the Mac answered it; nil when there is none.
    public func loadDeck() -> Data? {
        lock.lock()
        let latest = deckPending ?? waiting["deck.json"]
        lock.unlock()
        return latest ?? read("deck.json")
    }

    /// Keeps the deck's look: written at most once in `deckInterval` (and by `flush()`).
    public func saveDeck(_ raw: Data) {
        lock.lock(); defer { lock.unlock() }
        guard !retired else { return }
        deckPending = raw
        if Date().timeIntervalSince(lastDeckWrite) >= deckInterval { release(deck: true) }
    }

    /// Lets the deck's pending look through to be written. Call with the lock held.
    private func release(deck: Bool) {
        guard deck, let raw = deckPending else { return }
        deckPending = nil
        lastDeckWrite = Date()
        waiting["deck.json"] = raw
        scheduleWrite()
    }

    // MARK: Channels

    /// A channel's last messages as a `/api/bus/messages` answer (decode it as `MessagesPage`).
    public func loadChannel(_ id: String) -> Data? { touched(Self.channelFile(id)) }

    /// Takes one answer of /api/bus/messages into the channel's file: its messages are added after those held (a
    /// number held is not added twice), the oldest dropped past `messageCap`. An answer whose last number is lower
    /// than the one held means the channel began again: it replaces the file. An answer that adds nothing and
    /// changes nothing writes nothing.
    public func mergeChannel(_ id: String, page raw: Data) {
        guard let new = Self.object(raw) else { return }
        let file = Self.channelFile(id)
        lock.lock(); defer { lock.unlock() }
        guard !retired else { return }
        let old = state(file)
        let heldLast = old?["last"] as? Int ?? 0
        let newLast = new["last"] as? Int ?? 0
        var messages = (old?["messages"] as? [[String: Any]]) ?? []
        if newLast < heldLast { messages = [] }
        let top = messages.compactMap { $0["seq"] as? Int }.max() ?? 0
        for m in (new["messages"] as? [[String: Any]] ?? []).sorted(by: { ($0["seq"] as? Int ?? 0) < ($1["seq"] as? Int ?? 0) })
        where (m["seq"] as? Int ?? 0) > top {
            messages.append(m)
        }
        if messages.count > Self.messageCap { messages.removeFirst(messages.count - Self.messageCap) }
        let next: [String: Any] = ["channel": id, "messages": messages, "last": newLast,
                                   "paused": new["paused"] as? Bool ?? false, "receipts": new["receipts"] as? [Any] ?? []]
        if let old, NSDictionary(dictionary: old).isEqual(to: next) { return }
        keep(file, next)
    }

    /// Forgets a channel's file (it began again, or is gone).
    public func forgetChannel(_ id: String) {
        let file = Self.channelFile(id)
        lock.lock(); defer { lock.unlock() }
        held[file] = nil
        waiting[file] = nil
        digests[file] = nil
        try? FileManager.default.removeItem(at: directory.appendingPathComponent(file))
    }

    // MARK: Conversations

    /// An agent's transcript tail as a `/api/chat` answer that starts the conversation over (decode it as `ChatPage`).
    public func loadChat(_ agent: String) -> Data? { touched(Self.chatFile(agent)) }

    /// Takes one answer of /api/chat into the agent's file: a reset (or another transcript file) replaces the
    /// entries, otherwise they are added after those held, the oldest dropped past `chatCap` (and `earlier` says so).
    /// An answer with nothing new writes nothing.
    public func mergeChat(_ agent: String, page raw: Data) {
        guard let new = Self.object(raw) else { return }
        let file = Self.chatFile(agent)
        lock.lock(); defer { lock.unlock() }
        guard !retired else { return }
        let old = state(file)
        let reset = (new["reset"] as? Bool ?? false) || old == nil || (old?["file"] as? String) != (new["file"] as? String)
        var entries = reset ? [] : ((old?["entries"] as? [[String: Any]]) ?? [])
        var earlier = reset ? (new["earlier"] as? Bool ?? false) : (old?["earlier"] as? Bool ?? false)
        entries += new["entries"] as? [[String: Any]] ?? []
        if entries.count > Self.chatCap {
            entries.removeFirst(entries.count - Self.chatCap)
            earlier = true
        }
        let offset = (new["offset"] as? NSNumber)?.int64Value ?? 0
        let next: [String: Any] = ["file": new["file"] as? String ?? "", "offset": offset, "entries": entries, "reset": true, "earlier": earlier]
        if let old, NSDictionary(dictionary: old).isEqual(to: next) { return }
        keep(file, next)
    }

    // MARK: Writing and removing

    /// Writes what waits, now (the deck's look too).
    public func flush() {
        lock.lock()
        release(deck: true)
        writing?.cancel()
        writing = nil
        let batch = waiting
        waiting = [:]
        let gen = generation
        lock.unlock()
        queue.sync { write(batch, generation: gen) }
    }

    /// Removes everything this Mac has here, and drops what is still waiting or being written. The cache is used
    /// again from the next thing it is given (a refused token is a reason to forget, not to stop remembering).
    public func clear() {
        lock.lock()
        generation += 1
        resetMemory()
        lock.unlock()
        queue.sync { try? FileManager.default.removeItem(at: directory) }
    }

    /// The Mac is unpaired: clear, and keep nothing from now on.
    func retire() {
        lock.lock()
        retired = true
        generation += 1
        resetMemory()
        lock.unlock()
        queue.sync { try? FileManager.default.removeItem(at: directory) }
    }

    /// Call with the lock held.
    private func resetMemory() {
        writing?.cancel()
        writing = nil
        waiting = [:]
        held = [:]
        digests = [:]
        deckPending = nil
        lastDeckWrite = .distantPast
        pruned = false
    }

    /// Once per launch, after the first good look at the deck: removes the files of channels and agents the deck no
    /// longer has. Anything else is left (the deck's own file).
    public func pruneOnce(channels: Set<String>, agents: Set<String>) {
        lock.lock()
        guard !pruned, !retired else { lock.unlock(); return }
        pruned = true
        let gen = generation
        lock.unlock()
        let keepChannels = Set(channels.map { Self.channelFile($0) }), keepChats = Set(agents.map { Self.chatFile($0) })
        queue.async { [self] in
            lock.lock(); let same = gen == generation; lock.unlock()
            guard same else { return }
            let names = (try? FileManager.default.contentsOfDirectory(atPath: directory.path)) ?? []
            for name in names {
                let stale = (name.hasPrefix("ch.") && !keepChannels.contains(name)) || (name.hasPrefix("chat.") && !keepChats.contains(name))
                if stale {
                    try? FileManager.default.removeItem(at: directory.appendingPathComponent(name))
                    lock.lock(); held[name] = nil; digests[name] = nil; lock.unlock()
                }
            }
        }
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

    /// The file's bytes, and it counts as used now (so the cap puts out the ones used longest ago).
    private func touched(_ file: String) -> Data? {
        let data = read(file)
        if data != nil { try? FileManager.default.setAttributes([.modificationDate: Date()], ofItemAtPath: directory.appendingPathComponent(file).path) }
        return data
    }

    /// The file's bytes: what waits to be written, else what the disk has.
    private func read(_ file: String) -> Data? {
        lock.lock()
        if let w = waiting[file] { lock.unlock(); return w }
        lock.unlock()
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

    /// Call with the lock held.
    private func scheduleWrite() {
        guard writing == nil else { return }
        let item = DispatchWorkItem { [weak self] in
            guard let self else { return }
            self.lock.lock()
            // the deck's look, if its time has come, goes with this batch
            if let raw = self.deckPending, Date().timeIntervalSince(self.lastDeckWrite) >= self.deckInterval {
                self.waiting["deck.json"] = raw
                self.deckPending = nil
                self.lastDeckWrite = Date()
            }
            let batch = self.waiting
            self.waiting = [:]
            self.writing = nil
            let gen = self.generation
            self.lock.unlock()
            self.write(batch, generation: gen)
        }
        writing = item
        queue.asyncAfter(deadline: .now() + Self.writeDelay, execute: item)
    }

    private func write(_ batch: [String: Data], generation gen: Int) {
        guard !batch.isEmpty else { return }
        let fm = FileManager.default
        var folder = directory
        do {
            try fm.createDirectory(at: folder, withIntermediateDirectories: true)
            var values = URLResourceValues()
            values.isExcludedFromBackup = true
            try? folder.setResourceValues(values)
            for (name, data) in batch {
                let digest = Data(SHA256.hash(data: data))
                lock.lock()
                let dropped = gen != generation || retired
                let same = digests[name] == digest
                lock.unlock()
                if dropped { return }
                if same { continue } // the very bytes that were written last
                try data.write(to: folder.appendingPathComponent(name), options: [.atomic])
                lock.lock(); if gen == generation { digests[name] = digest }; lock.unlock()
            }
            enforceCaps()
        } catch {
            // A cache that cannot be written is only a cache that is not there.
        }
    }

    /// Keeps the folder to `maxFiles` files of channels and agents and `maxBytes` bytes: the ones used longest ago
    /// (by modification time, which a load renews) are removed first. The deck's file is not counted.
    private func enforceCaps() {
        let fm = FileManager.default
        let names = ((try? fm.contentsOfDirectory(atPath: directory.path)) ?? []).filter { $0 != "deck.json" && $0.hasSuffix(".json") }
        var files: [(name: String, size: Int, used: Date)] = names.compactMap { name in
            guard let a = try? fm.attributesOfItem(atPath: directory.appendingPathComponent(name).path) else { return nil }
            return (name, (a[.size] as? Int) ?? 0, (a[.modificationDate] as? Date) ?? .distantPast)
        }
        var total = files.reduce(0) { $0 + $1.size }
        files.sort { $0.used < $1.used }
        while !files.isEmpty, files.count > maxFiles || total > maxBytes {
            let out = files.removeFirst()
            total -= out.size
            try? fm.removeItem(at: directory.appendingPathComponent(out.name))
            lock.lock(); held[out.name] = nil; digests[out.name] = nil; lock.unlock()
        }
    }
}
