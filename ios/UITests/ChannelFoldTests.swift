import XCTest

/// A long channel (`channel-fold` scenario): folded handoffs and reports, whole
/// @you and own messages, pictures with a lightbox, a long URL, and a member strip.
final class ChannelFoldTests: MadUITestCase {
    private var showMore: XCUIElementQuery { web.buttons.matching(NSPredicate(format: "label == %@", "Show more")) }
    private var showLess: XCUIElementQuery { web.buttons.matching(NSPredicate(format: "label == %@", "Show less")) }

    override func setUp() {
        super.setUp()
        launch()
        waitForHome()
        // The scenario's one channel, “fold”, in 群聊.
        let row = web.buttons["fold"]
        XCTAssertTrue(row.waitForExistence(timeout: 10), "no “fold” row to open")
        row.tap()
        XCTAssertTrue(element("发消息给 fold").firstMatch.waitForExistence(timeout: 10), "the channel did not open (no composer)")
        settle(1)
    }

    func testHandoffAndDoneAreFoldedAndShowMoreExpandsThenShowLessFolds() {
        XCTAssertEqual(showMore.count, 2, "the handoff and the done report should each be folded: \(showMore.count) “Show more”")
        shot("folded")
        showMore.firstMatch.tap()
        settle()
        XCTAssertGreaterThanOrEqual(showLess.count, 1, "“Show less” did not appear after Show more")
        XCTAssertEqual(showMore.count, 1, "Show more should have expanded just that one")
        shot("one expanded")
        showLess.firstMatch.tap()
        settle()
        XCTAssertEqual(showMore.count, 2, "Show less did not fold it again")
        XCTAssertEqual(showLess.count, 0)
    }

    func testAtYouAndOwnMessagesAreWholeAndNotFolded() {
        XCTAssertTrue(element("@you").firstMatch.exists, "no @you message")
        XCTAssertTrue(element("请看一下 #88，合并前需要你确认").firstMatch.exists, "the @you message is cut or missing")
        XCTAssertTrue(element("移动端我用了一下，字体太小了，群里也没法发图片").firstMatch.exists, "the person's first message is cut or missing")
        XCTAssertTrue(element("收到，谢谢大家").firstMatch.exists, "the person's second message is cut or missing")
        // Only the two long reports are folded: neither @you nor the person's own messages offer Show more.
        XCTAssertEqual(showMore.count, 2)
        shot("@you and own messages whole")
    }

    func testPicturesShowAndATapOpensTheLightbox() {
        // The pictures are not in the accessibility tree (no alt text), so they are found
        // by where they are: under the message that says “两张截图”.
        let caption = element("两张截图：左边是修改前，右边是修改后。").firstMatch
        XCTAssertTrue(caption.waitForExistence(timeout: 10), "no message with the two screenshots")
        caption.tap() // brings it into view
        settle(1)
        shot("pictures")
        let before = web.descendants(matching: .any).count
        let spot = web.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: 140, dy: caption.frame.maxY + 50))
        spot.tap()
        settle(1.5)
        shot("lightbox")
        let after = web.descendants(matching: .any).count
        XCTAssertNotEqual(before, after, "tapping the picture changed nothing: no lightbox")
        // Close it with a tap on the backdrop, and the channel is back.
        web.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap()
        settle()
        XCTAssertTrue(element("发消息给 fold").firstMatch.waitForExistence(timeout: 5), "the lightbox did not close")
    }

    func testALongURLDoesNotWidenThePage() {
        let link = web.links.firstMatch
        XCTAssertTrue(link.waitForExistence(timeout: 10), "no link in the channel")
        XCTAssertLessThanOrEqual(link.frame.maxX, screenFrame.maxX, "the long URL runs past the screen: ends at \(link.frame.maxX)")
        let hbar = web.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH %@", "Horizontal scroll bar")).firstMatch
        if hbar.exists { XCTAssertTrue(hbar.label.contains("1 page"), "the page scrolls sideways: “\(hbar.label)”") }
        shot("long URL, no sideways scroll")
    }

    func testTheMemberStripShowsAPlusNAndATapListsEightMembers() {
        let strip = web.buttons.matching(NSPredicate(format: "label MATCHES %@", ".*\\+[0-9]+")).firstMatch
        XCTAssertTrue(strip.waitForExistence(timeout: 10), "no +n in the member strip")
        XCTAssertTrue(strip.label.hasSuffix("+2"), "the strip should say +2 for 8 members (6 shown): “\(strip.label)”")
        shot("member strip")
        strip.tap()
        settle(1)
        shot("members listed")
        // The list names all eight; four of them speak nowhere else in the channel.
        for name in ["architect", "builder", "reviewer", "designer", "tester", "docs", "ops", "kim"] {
            XCTAssertTrue(elements(containing: name, in: web).firstMatch.waitForExistence(timeout: 5), "the member list lacks “\(name)”")
        }
        XCTAssertGreaterThanOrEqual(elements(containing: "Idle", in: web).count, 8, "fewer than eight members are listed")
    }
}
