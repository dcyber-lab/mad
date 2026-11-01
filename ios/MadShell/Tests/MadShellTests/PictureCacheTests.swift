import XCTest
@testable import MadShell

final class PictureCacheTests: XCTestCase {
    private func blob(_ n: Int) -> Data { Data(repeating: 1, count: n) }

    func testEvictsLeastRecentlyUsedByBytes() async {
        let cache = PictureCache(budget: 100)
        _ = await cache.data(for: "a") { self.blob(40) }
        _ = await cache.data(for: "b") { self.blob(40) }
        _ = await cache.data(for: "a") { XCTFail("a is held"); return Data() } // a is now the newest
        _ = await cache.data(for: "c") { self.blob(40) } // 120 > 100: b goes
        let total = await cache.totalBytes, count = await cache.count
        XCTAssertEqual(total, 80)
        XCTAssertEqual(count, 2)
        let counter = Counter()
        _ = await cache.data(for: "b") { await counter.bump(); return self.blob(40) }
        let refetched = await counter.n
        XCTAssertEqual(refetched, 1)
    }

    func testPictureOverTheBudgetIsReturnedButNotKept() async {
        let cache = PictureCache(budget: 10)
        let d = await cache.data(for: "big") { self.blob(50) }
        XCTAssertEqual(d?.count, 50)
        let count = await cache.count
        XCTAssertEqual(count, 0)
    }

    func testOneFetchForConcurrentAsks() async {
        let cache = PictureCache(budget: 1000)
        let counter = Counter()
        await withTaskGroup(of: Void.self) { group in
            for _ in 0..<5 {
                group.addTask {
                    _ = await cache.data(for: "x") {
                        await counter.bump()
                        try await Task.sleep(for: .milliseconds(50))
                        return Data(repeating: 2, count: 10)
                    }
                }
            }
        }
        let n = await counter.n
        XCTAssertEqual(n, 1)
    }

    private actor Counter { var n = 0; func bump() { n += 1 } }
}
