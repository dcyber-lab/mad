import XCTest
@testable import MadKit

// Files in a temporary folder of their own; each test makes it and removes it.
private func tempRoot() -> URL {
    FileManager.default.temporaryDirectory.appendingPathComponent("mad-cache-test-" + UUID().uuidString, isDirectory: true)
}

/// A /api/bus/messages answer with messages `from...to`.
private func page(_ from: Int, _ to: Int, channel: String = "fold", last: Int? = nil, paused: Bool = false) -> Data {
    let ms = from > to ? [] : (from...to).map { #"{"seq":\#($0),"at":"2026-11-01T10:00:00+08:00","from":{"id":"kim","label":"kim"},"kind":"text","text":"m\#($0)"}"# }
    let json = #"{"channel":"\#(channel)","last":\#(last ?? to),"paused":\#(paused),"messages":[\#(ms.joined(separator: ","))],"receipts":[]}"#
    return Data(json.utf8)
}

private func decoded(_ data: Data?) throws -> MessagesPage {
    try JSONDecoder().decode(MessagesPage.self, from: try XCTUnwrap(data))
}

/// A /api/chat answer with `n` text entries.
private func chatPage(file: String = "f1", offset: Int64, n: Int, reset: Bool = false, earlier: Bool = false, base: Int = 0) -> Data {
    let es = n == 0 ? [] : (1...n).map { #"{"kind":"text","text":"e\#(base + $0)"}"# }
    return Data(#"{"file":"\#(file)","offset":\#(offset),"entries":[\#(es.joined(separator: ","))],"reset":\#(reset),"earlier":\#(earlier)}"#.utf8)
}

final class SnapshotCacheTests: XCTestCase {
    fileprivate var root = tempRoot()

    override func setUp() { root = tempRoot() }
    override func tearDown() { try? FileManager.default.removeItem(at: root) }

    private func cache(_ mac: String = "mac-1") -> SnapshotCache { SnapshotCache(root: root, mac: mac) }

    func testNothingIsKeptUntilSomethingIs() {
        let c = cache()
        XCTAssertNil(c.loadDeck())
        XCTAssertNil(c.loadChannel("fold"))
        XCTAssertNil(c.loadChat("a1"))
    }

    func testAChannelIsReadBackAfterARestart() throws {
        let c = cache()
        c.mergeChannel("fold", page: page(1, 3, paused: true))
        c.flush()
        let again = try decoded(cache().loadChannel("fold")) // another instance: what the disk has
        XCTAssertEqual(again.messages.map(\.seq), [1, 2, 3])
        XCTAssertEqual(again.last, 3)
        XCTAssertTrue(again.paused)
    }

    func testPagesAreAppendedAndANumberHeldIsNotAddedTwice() throws {
        let c = cache()
        c.mergeChannel("fold", page: page(1, 3))
        c.mergeChannel("fold", page: page(4, 5))
        c.mergeChannel("fold", page: page(3, 5)) // overlaps what is held
        let p = try decoded(c.loadChannel("fold"))
        XCTAssertEqual(p.messages.map(\.seq), [1, 2, 3, 4, 5])
        XCTAssertEqual(p.last, 5)
    }

    func testAChannelKeepsItsLast200Messages() throws {
        let c = cache()
        c.mergeChannel("fold", page: page(1, 150))
        c.mergeChannel("fold", page: page(151, 300))
        c.flush()
        let p = try decoded(cache().loadChannel("fold"))
        XCTAssertEqual(p.messages.count, SnapshotCache.messageCap)
        XCTAssertEqual(p.messages.first?.seq, 101)
        XCTAssertEqual(p.messages.last?.seq, 300)
        XCTAssertEqual(p.last, 300)
    }

    func testAChannelThatBeganAgainReplacesTheFile() throws {
        let c = cache()
        c.mergeChannel("fold", page: page(1, 10))
        c.mergeChannel("fold", page: page(1, 3)) // the Mac's last is lower than ours
        XCTAssertEqual(try decoded(c.loadChannel("fold")).messages.map(\.seq), [1, 2, 3])
    }

    func testAnIdThatIsNotAFileNameIsStillOneFile() {
        let c = cache()
        c.mergeChannel("../../dm.a/b", page: page(1, 1, channel: "dm"))
        c.flush()
        let files = (try? FileManager.default.contentsOfDirectory(atPath: c.directory.path)) ?? []
        XCTAssertEqual(files.count, 1)
        XCTAssertFalse(files[0].contains("/"))
        XCTAssertNotNil(c.loadChannel("../../dm.a/b"))
    }

    func testATranscriptKeepsItsTailAndGoesOnFromItsOffset() throws {
        let c = cache()
        c.mergeChat("a1", page: chatPage(offset: 100, n: 3, reset: true, earlier: true))
        c.mergeChat("a1", page: chatPage(offset: 160, n: 2, base: 3))
        c.flush()
        let p = try JSONDecoder().decode(ChatPage.self, from: try XCTUnwrap(cache().loadChat("a1")))
        XCTAssertEqual(p.entries.count, 5)
        XCTAssertEqual(p.offset, 160)
        XCTAssertEqual(p.file, "f1")
        XCTAssertTrue(p.earlier)
        XCTAssertTrue(p.reset, "it starts the conversation over when it is applied")
        var t = ChatTranscript()
        t.apply(p)
        XCTAssertEqual(t.offset, 160, "the looks go on from where the cache stopped")
    }

    func testATranscriptKeepsItsLast200EntriesAndSaysEarlierWasLeftOut() throws {
        let c = cache()
        c.mergeChat("a1", page: chatPage(offset: 1, n: 150, reset: true))
        c.mergeChat("a1", page: chatPage(offset: 2, n: 150, base: 150))
        let p = try JSONDecoder().decode(ChatPage.self, from: try XCTUnwrap(c.loadChat("a1")))
        XCTAssertEqual(p.entries.count, SnapshotCache.chatCap)
        XCTAssertTrue(p.earlier)
    }

    func testAnotherTranscriptFileOrAResetReplacesTheEntries() throws {
        let c = cache()
        c.mergeChat("a1", page: chatPage(offset: 1, n: 4, reset: true))
        c.mergeChat("a1", page: chatPage(file: "f2", offset: 9, n: 2))
        XCTAssertEqual(try JSONDecoder().decode(ChatPage.self, from: try XCTUnwrap(c.loadChat("a1"))).entries.count, 2)
    }

    func testClearingRemovesTheMacsFilesAndWhatWaitsIsNotWritten() throws {
        let c = cache()
        c.saveDeck(Data(#"{"machine":"m"}"#.utf8))
        c.mergeChannel("fold", page: page(1, 2))
        c.flush()
        let other = cache("mac-2")
        other.mergeChannel("fold", page: page(1, 2))
        other.flush()
        c.mergeChannel("fold", page: page(3, 4)) // waits
        c.clear()
        XCTAssertFalse(FileManager.default.fileExists(atPath: c.directory.path))
        XCTAssertNil(c.loadChannel("fold"))
        let waits = expectation(description: "past the write delay")
        DispatchQueue.global().asyncAfter(deadline: .now() + SnapshotCache.writeDelay + 0.4) { waits.fulfill() }
        wait(for: [waits], timeout: 5)
        XCTAssertFalse(FileManager.default.fileExists(atPath: c.directory.path), "what waited was not written after the clear")
        XCTAssertNotNil(other.loadChannel("fold"), "another Mac's cache is not touched")
    }

    func testWritesAreWaitedForAMomentAndMerged() throws {
        let c = cache()
        c.mergeChannel("fold", page: page(1, 2))
        XCTAssertFalse(FileManager.default.fileExists(atPath: c.directory.appendingPathComponent(SnapshotCache.channelFile("fold")).path),
                       "not written at once")
        XCTAssertNotNil(c.loadChannel("fold"), "but read back at once")
        let written = expectation(description: "written")
        DispatchQueue.global().asyncAfter(deadline: .now() + SnapshotCache.writeDelay + 0.5) { written.fulfill() }
        wait(for: [written], timeout: 5)
        XCTAssertTrue(FileManager.default.fileExists(atPath: c.directory.appendingPathComponent(SnapshotCache.channelFile("fold")).path))
    }

    func testTheFixtureServerIsNotKept() {
        let fixture = Mac(id: Mac.fixtureID, name: "Fixture", baseURL: URL(string: "http://127.0.0.1:1")!, token: String(repeating: "0", count: 32))
        XCTAssertNil(SnapshotCache.standard(for: fixture))
        XCTAssertEqual(FixtureHook.endpoint(environment: ["MAD_FIXTURE_URL": "http://127.0.0.1:1"], arguments: [])?.mac().id, Mac.fixtureID)
        XCTAssertNotNil(SnapshotCache.standard(for: Mac(name: "m", baseURL: URL(string: "https://x")!, token: "t")))
    }

    func testNoTokenIsWrittenAnywhere() throws {
        let secret = "0123456789abcdef0123456789abcdef"
        let mac = Mac(name: "m", baseURL: URL(string: "https://x")!, token: secret)
        let c = SnapshotCache(root: root, mac: mac.id.uuidString)
        c.saveDeck(Data(#"{"machine":"m","projects":[]}"#.utf8))
        c.mergeChannel("fold", page: page(1, 3))
        c.mergeChat("a1", page: chatPage(offset: 5, n: 2, reset: true))
        c.flush()
        let names = try FileManager.default.contentsOfDirectory(atPath: c.directory.path)
        XCTAssertFalse(c.directory.path.contains(secret))
        for name in names {
            let text = String(decoding: try Data(contentsOf: c.directory.appendingPathComponent(name)), as: UTF8.self)
            XCTAssertFalse(text.contains(secret), "\(name) holds the token")
        }
    }
}

extension SnapshotCacheTests {
    /// The modification dates of the files in a Mac's folder.
    fileprivate func stamps(_ c: SnapshotCache) -> [String: Date] {
        let names = (try? FileManager.default.contentsOfDirectory(atPath: c.directory.path)) ?? []
        return Dictionary(uniqueKeysWithValues: names.compactMap { n in
            (try? FileManager.default.attributesOfItem(atPath: c.directory.appendingPathComponent(n).path)[.modificationDate] as? Date).map { (n, $0) }
        })
    }

    func testAnAnswerThatAddsNothingWritesNothing() throws {
        let c = SnapshotCache(root: root, mac: "m")
        c.mergeChannel("fold", page: page(1, 3))
        c.mergeChat("a1", page: chatPage(offset: 10, n: 2, reset: true))
        c.flush()
        let before = stamps(c)
        XCTAssertEqual(before.count, 2)
        Thread.sleep(forTimeInterval: 1.1) // so a rewrite would show in the modification time
        c.mergeChannel("fold", page: page(4, 3, last: 3)) // the same last, no news
        c.mergeChannel("fold", page: page(1, 3)) // all of it held already
        c.mergeChat("a1", page: chatPage(offset: 10, n: 0)) // no entries, the same offset
        let waits = expectation(description: "past the write delay")
        DispatchQueue.global().asyncAfter(deadline: .now() + SnapshotCache.writeDelay + 0.4) { waits.fulfill() }
        wait(for: [waits], timeout: 5)
        c.flush()
        XCTAssertEqual(stamps(c), before, "nothing was written")
        // and a change is written: the channel is paused now
        c.mergeChannel("fold", page: page(4, 3, last: 3, paused: true))
        c.flush()
        XCTAssertNotEqual(stamps(c)[SnapshotCache.channelFile("fold")], before[SnapshotCache.channelFile("fold")])
        XCTAssertTrue(try decoded(c.loadChannel("fold")).paused)
    }

    func testTheSameBytesAreNotWrittenAgain() throws {
        let c = SnapshotCache(root: root, mac: "m", deckInterval: 0)
        c.saveDeck(Data(#"{"machine":"m"}"#.utf8))
        c.flush()
        let file = c.directory.appendingPathComponent("deck.json")
        let first = try FileManager.default.attributesOfItem(atPath: file.path)[.modificationDate] as? Date
        Thread.sleep(forTimeInterval: 1.1)
        c.saveDeck(Data(#"{"machine":"m"}"#.utf8))
        c.flush()
        XCTAssertEqual(try FileManager.default.attributesOfItem(atPath: file.path)[.modificationDate] as? Date, first)
    }

    func testTheDeckIsWrittenOnceInAWhileNotOnEveryPoll() throws {
        let c = SnapshotCache(root: root, mac: "m", deckInterval: 60)
        let file = c.directory.appendingPathComponent("deck.json")
        c.saveDeck(Data(#"{"machine":"one"}"#.utf8)) // the first look goes through
        let waits = expectation(description: "written")
        DispatchQueue.global().asyncAfter(deadline: .now() + SnapshotCache.writeDelay + 0.4) { waits.fulfill() }
        wait(for: [waits], timeout: 5)
        XCTAssertTrue(FileManager.default.fileExists(atPath: file.path))
        for i in 2...6 { c.saveDeck(Data(#"{"machine":"m\#(i)"}"#.utf8)) } // five polls within the interval
        let more = expectation(description: "past the delay again")
        DispatchQueue.global().asyncAfter(deadline: .now() + SnapshotCache.writeDelay + 0.4) { more.fulfill() }
        wait(for: [more], timeout: 5)
        XCTAssertTrue(String(decoding: try Data(contentsOf: file), as: UTF8.self).contains("one"), "not written within the interval")
        XCTAssertTrue(String(decoding: c.loadDeck() ?? Data(), as: UTF8.self).contains("m6"), "but read back as the latest")
        c.flush() // the app goes to the background
        XCTAssertTrue(String(decoding: try Data(contentsOf: file), as: UTF8.self).contains("m6"))
    }

    func testFilesOfChannelsAndAgentsThatAreGoneArePrunedOncePerLaunch() throws {
        let c = SnapshotCache(root: root, mac: "m")
        c.saveDeck(Data(#"{"machine":"m"}"#.utf8))
        for id in ["keep", "gone"] { c.mergeChannel(id, page: page(1, 1)) }
        for id in ["a-keep", "a-gone"] { c.mergeChat(id, page: chatPage(offset: 1, n: 1, reset: true)) }
        c.flush()
        c.pruneOnce(channels: ["keep"], agents: ["a-keep"])
        let settled = expectation(description: "pruned")
        DispatchQueue.global().asyncAfter(deadline: .now() + 0.5) { settled.fulfill() }
        wait(for: [settled], timeout: 5)
        let names = Set((try? FileManager.default.contentsOfDirectory(atPath: c.directory.path)) ?? [])
        XCTAssertEqual(names, ["deck.json", SnapshotCache.channelFile("keep"), SnapshotCache.chatFile("a-keep")])
        // a second call in the same launch does nothing, even for a file that is not in the lists
        c.mergeChannel("later", page: page(1, 1))
        c.flush()
        c.pruneOnce(channels: [], agents: [])
        let again = expectation(description: "again")
        DispatchQueue.global().asyncAfter(deadline: .now() + 0.4) { again.fulfill() }
        wait(for: [again], timeout: 5)
        XCTAssertNotNil(c.loadChannel("later"))
    }

    func testTheFolderIsHeldToItsFileCapAndTheLeastRecentlyUsedGoFirst() throws {
        let c = SnapshotCache(root: root, mac: "m", maxFiles: 3)
        for (i, id) in ["a", "b", "c"].enumerated() {
            c.mergeChannel(id, page: page(1, 1))
            c.flush()
            // the files' ages: a oldest, then b, then c
            try FileManager.default.setAttributes([.modificationDate: Date(timeIntervalSinceNow: Double(i - 10))],
                                                  ofItemAtPath: c.directory.appendingPathComponent(SnapshotCache.channelFile(id)).path)
        }
        XCTAssertNotNil(c.loadChannel("a")) // a is used now: b is the oldest
        c.mergeChannel("d", page: page(1, 1))
        c.flush()
        let names = Set((try? FileManager.default.contentsOfDirectory(atPath: c.directory.path)) ?? [])
        XCTAssertEqual(names, [SnapshotCache.channelFile("a"), SnapshotCache.channelFile("c"), SnapshotCache.channelFile("d")])
    }

    func testTheFolderIsHeldToItsByteCap() throws {
        let c = SnapshotCache(root: root, mac: "m", maxBytes: 1500)
        for (i, id) in ["a", "b", "c", "d"].enumerated() {
            c.mergeChannel(id, page: page(1, 6))
            c.flush()
            try FileManager.default.setAttributes([.modificationDate: Date(timeIntervalSinceNow: Double(i - 10))],
                                                  ofItemAtPath: c.directory.appendingPathComponent(SnapshotCache.channelFile(id)).path)
        }
        let names = (try? FileManager.default.contentsOfDirectory(atPath: c.directory.path)) ?? []
        let total = names.reduce(0) { $0 + ((try? FileManager.default.attributesOfItem(atPath: c.directory.appendingPathComponent($1).path)[.size] as? Int) ?? 0) }
        XCTAssertLessThanOrEqual(total, 1500)
        XCTAssertFalse(names.contains(SnapshotCache.channelFile("a")), "the oldest went first")
    }

    func testAClearedCacheWorksAgain() throws {
        let c = SnapshotCache(root: root, mac: "m")
        c.mergeChannel("fold", page: page(1, 2))
        c.flush()
        c.clear() // a refused token
        XCTAssertNil(c.loadChannel("fold"))
        c.mergeChannel("fold", page: page(1, 3)) // paired again, the same object is still in use
        c.flush()
        XCTAssertEqual(try decoded(c.loadChannel("fold")).messages.map(\.seq), [1, 2, 3])
        XCTAssertTrue(FileManager.default.fileExists(atPath: c.directory.path))
    }

    func testAMergeBeganBeforeAClearIsNotWrittenAfterIt() throws {
        let c = SnapshotCache(root: root, mac: "m")
        c.mergeChannel("fold", page: page(1, 2)) // waits to be written
        c.clear()
        let waits = expectation(description: "past the write delay")
        DispatchQueue.global().asyncAfter(deadline: .now() + SnapshotCache.writeDelay + 0.4) { waits.fulfill() }
        wait(for: [waits], timeout: 5)
        XCTAssertFalse(FileManager.default.fileExists(atPath: c.directory.path), "the old write was dropped")
    }

    func testAnUnpairedMacKeepsNothingAndTheSameIdGetsAFreshCache() throws {
        let mac = Mac(name: "a", baseURL: URL(string: "https://a")!, token: "ta")
        let before = try XCTUnwrap(SnapshotCache.standard(for: mac))
        SnapshotCache.clear(mac: mac) // unpaired
        before.mergeChannel("fold", page: page(1, 2)) // a request that was still on its way
        before.flush()
        XCTAssertFalse(FileManager.default.fileExists(atPath: before.directory.path), "nothing is kept for an unpaired Mac")
        let again = try XCTUnwrap(SnapshotCache.standard(for: mac)) // paired again, the same id
        XCTAssertFalse(again === before)
        again.mergeChannel("fold", page: page(1, 2))
        again.flush()
        XCTAssertNotNil(again.loadChannel("fold"))
        SnapshotCache.clear(mac: mac)
    }
}

// MARK: through the client and the stores

/// A session whose answers are scripted by path.
private final class Scripted: URLProtocol {
    nonisolated(unsafe) static var answers: [String: (Int, Data)] = [:]
    nonisolated(unsafe) static var asked: [URL] = []
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        Self.asked.append(request.url!)
        let (code, data) = Self.answers[request.url!.path] ?? (404, Data())
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: request.url!, statusCode: code, httpVersion: nil, headerFields: nil)!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: data)
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

@MainActor
final class CachedStoresTests: XCTestCase {
    private var root = tempRoot()

    override func setUp() {
        root = tempRoot()
        Scripted.answers = [:]
        Scripted.asked = []
    }

    override func tearDown() { try? FileManager.default.removeItem(at: root) }

    private func client(_ mac: Mac = Mac(name: "m", baseURL: URL(string: "https://box")!, token: "t")) -> (MadClient, SnapshotCache) {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [Scripted.self]
        let cache = SnapshotCache(root: root, mac: mac.id.uuidString)
        return (MadClient(mac: mac, session: URLSession(configuration: config), cache: cache), cache)
    }

    func testAChannelOpensFromTheCacheAndAsksOnlyForWhatCameAfter() async throws {
        let (api, cache) = client()
        Scripted.answers["/api/bus/messages"] = (200, page(1, 5))
        let first = ChannelStore(id: "fold", api: api)
        XCTAssertTrue(first.messages.isEmpty, "nothing cached on the very first open")
        await first.refresh()
        XCTAssertEqual(first.last, 5)
        cache.flush()

        // a new start: the same Mac, a new cache object, a new store
        let (api2, _) = client(api.mac)
        let again = ChannelStore(id: "fold", api: api2)
        XCTAssertEqual(again.messages.map(\.seq), [1, 2, 3, 4, 5], "it draws at once")
        XCTAssertTrue(again.fromCache)
        XCTAssertEqual(again.last, 5)
        Scripted.answers["/api/bus/messages"] = (200, page(6, 6))
        Scripted.asked = []
        await again.refresh()
        let since = URLComponents(url: try XCTUnwrap(Scripted.asked.first), resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == "since" }?.value
        XCTAssertEqual(since, "5", "it asks only for what came after the last held")
        XCTAssertEqual(again.messages.map(\.seq), [1, 2, 3, 4, 5, 6])
        XCTAssertFalse(again.fromCache)
    }

    func testAChannelThatBeganAgainOnTheMacStartsOverFromTheFirst() async throws {
        let (api, cache) = client()
        Scripted.answers["/api/bus/messages"] = (200, page(1, 8))
        let store = ChannelStore(id: "fold", api: api)
        await store.refresh()
        cache.flush()
        // the Mac's channel has only 3 messages now (its last number is 3): the first answer says so
        Scripted.answers["/api/bus/messages"] = (200, page(1, 3))
        await store.refresh()
        XCTAssertEqual(store.messages.map(\.seq), [1, 2, 3])
        XCTAssertEqual(store.last, 3)
    }

    func testTheDeckComesFromTheCacheBeforeTheFirstLook() async throws {
        let (api, cache) = client()
        Scripted.answers["/api/deck"] = (200, Data(#"{"machine":"Studio","projects":[]}"#.utf8))
        let first = DeckStore(api: api)
        XCTAssertNil(first.deck)
        await first.refresh()
        cache.flush()
        let (api2, _) = client(api.mac)
        let again = DeckStore(api: api2)
        XCTAssertEqual(again.deck?.machine, "Studio", "the home draws at once")
        XCTAssertEqual(again.link, .connecting)
    }

    func testATranscriptComesFromTheCacheAndGoesOnFromItsOffset() async throws {
        let (api, cache) = client()
        Scripted.answers["/api/chat"] = (200, chatPage(offset: 80, n: 4, reset: true))
        let first = ChatSession(agentID: "a1", api: api)
        await first.look()
        cache.flush()
        let (api2, _) = client(api.mac)
        let again = ChatSession(agentID: "a1", api: api2)
        XCTAssertEqual(again.transcript.entries.count, 4)
        XCTAssertEqual(again.transcript.offset, 80)
        XCTAssertTrue(again.loaded)
        Scripted.asked = []
        Scripted.answers["/api/chat"] = (200, chatPage(offset: 90, n: 1, base: 4))
        await again.look()
        let from = URLComponents(url: try XCTUnwrap(Scripted.asked.first), resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == "from" }?.value
        XCTAssertEqual(from, "80")
        XCTAssertEqual(again.transcript.entries.count, 5)
    }

    func testARefusedTokenTakesTheMacsCacheWithIt() async throws {
        let (api, cache) = client()
        Scripted.answers["/api/bus/messages"] = (200, page(1, 3))
        let store = ChannelStore(id: "fold", api: api)
        await store.refresh()
        cache.flush()
        XCTAssertTrue(FileManager.default.fileExists(atPath: cache.directory.path))
        Scripted.answers["/api/bus/messages"] = (403, Data())
        await store.refresh()
        XCTAssertEqual(store.link, .refused)
        XCTAssertFalse(FileManager.default.fileExists(atPath: cache.directory.path))
    }

    func testUnpairingAMacRemovesItsCacheAndOnlyIts() throws {
        let mac = Mac(name: "a", baseURL: URL(string: "https://a")!, token: "ta")
        let other = Mac(name: "b", baseURL: URL(string: "https://b")!, token: "tb")
        for m in [mac, other] {
            let c = SnapshotCache(root: root, mac: m.id.uuidString)
            c.mergeChannel("fold", page: page(1, 2))
            c.flush()
        }
        let book = PairingBook(secrets: NullSecrets(), macs: [mac, other], deviceName: "d", cacheRoot: root,
                               makeToken: { "x" }, send: { _ in PairResult(id: "i") })
        book.remove(mac)
        XCTAssertFalse(FileManager.default.fileExists(atPath: root.appendingPathComponent(mac.id.uuidString).path))
        XCTAssertTrue(FileManager.default.fileExists(atPath: root.appendingPathComponent(other.id.uuidString).path))
    }
}

private final class NullSecrets: SecretStore {
    func set(_ value: String, for account: String) throws {}
    func get(_ account: String) -> String? { nil }
    func delete(_ account: String) {}
}
