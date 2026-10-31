import XCTest
@testable import MadKit

private struct SaveFailed: Error {}

/// A secret store in memory. A failing `set` leaves the old value, like the Keychain's.
private final class FakeSecrets: SecretStore {
    var items: [String: String] = [:]
    var failSets = false

    func set(_ value: String, for account: String) throws {
        if failSets { throw SaveFailed() }
        items[account] = value
    }
    func get(_ account: String) -> String? { items[account] }
    func delete(_ account: String) { items[account] = nil }
}

private let base = URL(string: "https://box.ts.net:7443")!
private let pairCode = "fedcba9876543210fedcba9876543210"
private let tokenA = "0123456789abcdef0123456789abcdef"
private let tokenB = "aaaaaaaaaaaaaaaabbbbbbbbbbbbbbbb"

@MainActor
final class PairingBookTests: XCTestCase {
    private var secrets = FakeSecrets()
    private var sent: [PendingPairing] = []
    private var answer: (PendingPairing) async throws -> PairResult = { _ in PairResult(id: "dev-1") }
    private var tokens = [tokenA, tokenB]

    private func book(macs: [Mac] = []) -> PairingBook {
        secrets = FakeSecrets()
        for m in macs { secrets.items[m.id.uuidString] = m.token }
        sent = []
        return PairingBook(secrets: secrets, macs: macs, deviceName: "Heng's iPhone",
                           makeToken: { [unowned self] in self.tokens.removeFirst() },
                           send: { [unowned self] p in
                               self.sent.append(p)
                               return try await self.answer(p)
                           })
    }

    private let link = PairingLink.pair(base: base, code: pairCode)

    func testPairingStoresTheMacAndClearsPending() async throws {
        let b = book()
        try await b.add(link)
        XCTAssertEqual(b.pending, [])
        XCTAssertEqual(b.macs.count, 1)
        XCTAssertEqual(b.macs[0].token, tokenA)
        XCTAssertEqual(b.macs[0].deviceID, "dev-1")
        XCTAssertEqual(b.macs[0].name, "box")
        XCTAssertEqual(sent.map(\.name), ["Heng's iPhone"])
        XCTAssertEqual(secrets.items.values.sorted(), [tokenA], "only the Mac's token stays")
    }

    func testNetworkErrorKeepsPendingAndRetryReusesCodeAndToken() async throws {
        let b = book()
        answer = { _ in throw ConnectionError.offline }
        do { try await b.add(link); XCTFail("expected offline") } catch {
            XCTAssertEqual(error as? ConnectionError, .offline)
        }
        XCTAssertEqual(b.pending.count, 1)
        XCTAssertEqual(b.macs, [])
        let p = b.pending[0]
        XCTAssertEqual(secrets.items[PairingBook.account(p)], tokenA, "the token was saved before it was sent")

        answer = { _ in PairResult(id: "dev-1") }
        try await b.complete(p)
        XCTAssertEqual(sent.count, 2)
        XCTAssertEqual(sent[0], sent[1], "the retry is the same code, token and name")
        XCTAssertEqual(b.pending, [])
        XCTAssertEqual(b.macs[0].token, tokenA)
        XCTAssertNil(secrets.items[PairingBook.account(p)])
    }

    func testScanningTheSameQRAgainContinuesThePairing() async throws {
        let b = book()
        answer = { _ in throw ConnectionError.offline }
        try? await b.add(link)
        answer = { _ in PairResult(id: "dev-1") }
        try await b.add(link)
        XCTAssertEqual(sent.map(\.token), [tokenA, tokenA])
        XCTAssertEqual(tokens, [tokenB], "no second token was made")
        XCTAssertEqual(b.macs.count, 1)
    }

    func testRefusalDropsPending() async {
        let b = book()
        answer = { _ in throw ConnectionError.pairingRefused }
        do { try await b.add(link); XCTFail("expected refusal") } catch {
            XCTAssertEqual(error as? ConnectionError, .pairingRefused)
        }
        XCTAssertEqual(b.pending, [])
        XCTAssertEqual(b.macs, [])
        XCTAssertTrue(secrets.items.isEmpty)
    }

    func testDeletingDuringFlightWins() async throws {
        let b = book()
        var release: CheckedContinuation<PairResult, Error>?
        answer = { _ in try await withCheckedThrowingContinuation { release = $0 } }
        let task = Task { try await b.add(link) }
        while release == nil { await Task.yield() }
        b.drop(b.pending[0])
        release?.resume(returning: PairResult(id: "dev-1"))
        try await task.value
        XCTAssertEqual(b.macs, [], "a late answer for a deleted pairing installs nothing")
        XCTAssertEqual(b.pending, [])
        XCTAssertTrue(secrets.items.isEmpty)
    }

    func testFailedSaveOfThePendingTokenSendsNothing() async {
        let b = book()
        secrets.failSets = true
        do { try await b.add(link); XCTFail("expected a save error") } catch {
            XCTAssertTrue(error is SaveFailed)
        }
        XCTAssertEqual(sent, [])
        XCTAssertEqual(b.pending, [])
        XCTAssertEqual(b.macs, [])
    }

    func testFailedRepairKeepsTheOldToken() throws {
        let old = Mac(name: "box", baseURL: base, token: tokenA)
        let b = book(macs: [old])
        secrets.failSets = true
        XCTAssertThrowsError(try b.add(Mac(name: "box", baseURL: base, token: tokenB)))
        XCTAssertEqual(b.macs, [old])
        XCTAssertEqual(secrets.items[old.id.uuidString], tokenA)
    }

    func testRepairReplacesTheTokenAndKeepsIdAndName() throws {
        var old = Mac(name: "My Mac", baseURL: base, token: tokenA)
        old.deviceID = "dev-0"
        let b = book(macs: [old])
        try b.add(Mac(name: "box", baseURL: base, token: tokenB, deviceID: "dev-1"))
        XCTAssertEqual(b.macs.count, 1)
        XCTAssertEqual(b.macs[0].id, old.id)
        XCTAssertEqual(b.macs[0].name, "My Mac")
        XCTAssertEqual(b.macs[0].token, tokenB)
        XCTAssertEqual(secrets.items[old.id.uuidString], tokenB)
    }

    func testFailedSaveOfANewMacAddsNothing() {
        let b = book()
        secrets.failSets = true
        XCTAssertThrowsError(try b.add(Mac(name: "box", baseURL: base, token: tokenA)))
        XCTAssertEqual(b.macs, [])
    }

    func testOnChangeFires() async throws {
        let b = book()
        var n = 0
        b.onChange = { n += 1 }
        try await b.add(link)
        XCTAssertGreaterThanOrEqual(n, 2)
    }

    func testPairingTakesTheMachineNameFromTheReply() async throws {
        let b = book()
        answer = { _ in PairResult(id: "dev-1", machine: "Heng's MacBook Pro") }
        try await b.add(link)
        XCTAssertEqual(b.macs[0].name, "Heng's MacBook Pro")
        XCTAssertFalse(b.macs[0].renamed)
    }

    func testSnapshotNameReplacesTheDefaultButNotARename() throws {
        let m = Mac(name: "box", baseURL: base, token: tokenA)
        let b = book(macs: [m])
        b.adoptMachineName("Heng's MacBook Pro", for: m)
        XCTAssertEqual(b.macs[0].name, "Heng's MacBook Pro")
        b.rename(b.macs[0], to: "Office")
        XCTAssertTrue(b.macs[0].renamed)
        b.adoptMachineName("Something Else", for: b.macs[0])
        XCTAssertEqual(b.macs[0].name, "Office")
        b.adoptMachineName(nil, for: b.macs[0])
        XCTAssertEqual(b.macs[0].name, "Office")
    }

    func testRepairKeepsTheRenamedFlag() throws {
        let m = Mac(name: "Office", baseURL: base, token: tokenA, renamed: true)
        let b = book(macs: [m])
        try b.add(Mac(name: "Studio", baseURL: base, token: tokenB))
        XCTAssertEqual(b.macs[0].name, "Office")
        XCTAssertTrue(b.macs[0].renamed)
    }
}
