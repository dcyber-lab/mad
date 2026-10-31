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
        wait("the channel's messages did not come") { self.showMore.count >= 2 }
    }

    func testHandoffAndDoneAreFoldedAndShowMoreExpandsThenShowLessFolds() {
        // At least the handoff and the done report are folded (the fixture may fold more).
        let folded = showMore.count
        XCTAssertGreaterThanOrEqual(folded, 2, "the handoff and the done report should each be folded: \(folded) “Show more”")
        shot("folded")
        showMore.firstMatch.tap()
        wait("“Show less” did not appear after Show more") { self.showLess.count >= 1 }
        XCTAssertEqual(showMore.count, folded - 1, "Show more should have expanded just that one")
        shot("one expanded")
        showLess.firstMatch.tap()
        wait("Show less did not fold it again") { self.showMore.count == folded }
        XCTAssertEqual(showLess.count, 0)
    }

    func testAtYouAndOwnMessagesAreWholeAndNotFolded() {
        XCTAssertTrue(element("@you").firstMatch.exists, "no @you message")
        XCTAssertTrue(element("请看一下 #88，合并前需要你确认").firstMatch.exists, "the @you message is cut or missing")
        XCTAssertTrue(element("移动端我用了一下，字体太小了，群里也没法发图片").firstMatch.exists, "the person's first message is cut or missing")
        XCTAssertTrue(element("收到，谢谢大家").firstMatch.exists, "the person's second message is cut or missing")
        // The long reports are folded; the @you and the person's own messages are not (they are whole above).
        shot("@you and own messages whole")
    }

    func testPicturesShowAndATapOpensTheLightbox() {
        // Each picture is a button, "图片，来自 <sender>", holding an image with the same alt.
        let pictures = web.buttons.matching(NSPredicate(format: "label == %@", "图片，来自 reviewer"))
        XCTAssertTrue(pictures.firstMatch.waitForExistence(timeout: 10), "no picture button")
        XCTAssertEqual(pictures.count, 2, "the message has two pictures, fx-before and fx-after")
        let before = web.images.count
        pictures.firstMatch.tap() // brings it into view and opens it
        // The lightbox is one more image over the page (hittability cannot tell, so count them).
        wait("tapping the picture did not open a lightbox (images: \(before), now \(web.images.count))") { self.web.images.count == before + 1 }
        shot("lightbox")
        // A tap anywhere on the lightbox closes it, and the channel is back.
        web.coordinate(withNormalizedOffset: CGVector(dx: 0.9, dy: 0.08)).tap()
        wait("the lightbox did not close") { self.web.images.count == before }
        shot("closed again")
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
        XCTAssertTrue(elements(containing: "tester", in: web).firstMatch.waitForExistence(timeout: 10), "the member list did not open")
        shot("members listed")
        // The list names all eight; four of them speak nowhere else in the channel.
        for name in ["architect", "builder", "reviewer", "designer", "tester", "docs", "ops", "kim"] {
            XCTAssertTrue(elements(containing: name, in: web).firstMatch.waitForExistence(timeout: 5), "the member list lacks “\(name)”")
        }
        XCTAssertGreaterThanOrEqual(elements(containing: "Idle", in: web).count, 8, "fewer than eight members are listed")
    }

    // Nothing a message draws may run past the screen's edge. A clipped overflow is invisible in a
    // screenshot and in a scroll bar, so the bounds of every part of the channel are checked.

    func testNoPartOfTheChannelCrossesTheScreenEdgeInPortrait() {
        shot("channel, portrait")
        let past = partsPastTheScreenEdge()
        XCTAssertTrue(past.isEmpty, "\(past.count) part(s) of the channel cross the screen edge in portrait:\n" + past.prefix(12).joined(separator: "\n"))
    }

    func testNoPartOfTheChannelCrossesTheScreenEdgeInLandscape() {
        rotateToLandscape()
        XCTAssertTrue(element("发消息给 fold").firstMatch.waitForExistence(timeout: 10), "the channel did not lay out in landscape")
        shot("channel, landscape")
        let past = partsPastTheScreenEdge()
        XCTAssertTrue(past.isEmpty, "\(past.count) part(s) of the channel cross the screen edge in landscape:\n" + past.prefix(12).joined(separator: "\n"))
    }
}
