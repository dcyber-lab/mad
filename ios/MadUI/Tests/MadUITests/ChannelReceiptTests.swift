import XCTest
@testable import MadUI

final class ChannelReceiptTests: XCTestCase {
    private func receipt(_ steps: [ChannelReceipt.Step], paused: Bool = false) -> ChannelReceipt {
        ChannelReceipt(targets: steps.enumerated().map { .init(id: "a\($0.offset)", kind: "claude", name: "agent\($0.offset)", step: $0.element) }, paused: paused)
    }

    func testNobodyWoken() {
        let r = receipt([])
        XCTAssertEqual(r.word, "没有叫醒谁")
        XCTAssertFalse(r.isUnread)
        XCTAssertEqual(r.footer, "没有 @ 任何 agent，所以没有叫醒谁。@ 一个 agent 才会叫醒它。")
    }

    func testPausedWithSomeoneWaitingIsHeldAndBeatsEverythingButNobody() {
        let r = receipt([.read, .waiting], paused: true)
        XCTAssertEqual(r.word, "群已暂停，恢复后送达")
        XCTAssertFalse(r.isUnread)
        XCTAssertTrue(r.held)
        XCTAssertTrue(r.footer.hasSuffix("群已暂停，恢复后送达"))
        XCTAssertEqual(receipt([.read, .delivered], paused: true).word, "1 个未读", "paused, but nobody waits: nothing is held")
        XCTAssertEqual(receipt([.waiting, .waiting], paused: false).word, "待送达")
    }

    func testAllWaitingIsGreyAndNotUnread() {
        XCTAssertEqual(receipt([.waiting]).word, "待送达")
        XCTAssertEqual(receipt([.waiting, .waiting, .waiting]).word, "待送达")
        XCTAssertFalse(receipt([.waiting, .waiting]).isUnread)
    }

    func testUnseenIsBlueAndCountsWhenSeveralWereWoken() {
        XCTAssertEqual(receipt([.delivered]).word, "未读")
        XCTAssertTrue(receipt([.delivered]).isUnread)
        XCTAssertEqual(receipt([.delivered, .read]).word, "1 个未读")
        XCTAssertEqual(receipt([.waiting, .delivered, .read, .replied]).word, "2 个未读", "waiting and delivered are both unseen")
        XCTAssertTrue(receipt([.waiting, .read]).isUnread, "one waits, one has read: still unread, not 待送达")
        XCTAssertEqual(receipt([.waiting, .read]).word, "1 个未读")
    }

    func testAllSeen() {
        XCTAssertEqual(receipt([.read]).word, "已读")
        XCTAssertEqual(receipt([.replied]).word, "已回复")
        XCTAssertEqual(receipt([.read, .read]).word, "全部已读")
        XCTAssertEqual(receipt([.read, .replied]).word, "已回复")
        XCTAssertEqual(receipt([.replied, .replied]).word, "已回复")
        XCTAssertFalse(receipt([.read, .replied]).isUnread)
    }

    func testTargetsAreOrderedAndSpokenInWords() {
        let r = receipt([.waiting, .read, .delivered, .replied])
        XCTAssertEqual(r.targets.map(\.step), [.replied, .read, .delivered, .waiting])
        XCTAssertEqual(receipt([.read, .delivered]).spoken, "agent0 已读，agent1 未读")
        XCTAssertEqual(receipt([]).spoken, "没有叫醒谁")
        XCTAssertEqual(ChannelReceipt.state(.delivered), "已送达，还没看")
        XCTAssertEqual(ChannelReceipt.state(.waiting), "待送达 · 忙完这一轮就会收到")
    }
}
