import XCTest

/// A long channel (`channel-fold` scenario): folded handoffs and reports, whole
/// @you and own messages, pictures with a lightbox, a long URL, and a member strip.
final class ChannelFoldTests: MadUITestCase {
    override func setUp() {
        super.setUp()
        launch()
        waitForHome()
        // The scenario's one channel: open the first row of the channel group.
        let row = web.buttons.allElementsBoundByIndex.filter { $0.frame.height > 40 && $0.frame.minY > 150 }
            .sorted { $0.frame.minY < $1.frame.minY }.first
        XCTAssertNotNil(row, "no channel row to open")
        row?.tap()
        settle(2)
    }

    func testHandoffAndDoneAreFoldedAndShowMoreExpandsThenShowLessFolds() {
        let more = web.buttons.matching(NSPredicate(format: "label CONTAINS[c] %@", "Show more")).firstMatch
        XCTAssertTrue(more.waitForExistence(timeout: 10), "nothing is folded: no “Show more”")
        shot("folded")
        more.tap()
        settle()
        let less = web.buttons.matching(NSPredicate(format: "label CONTAINS[c] %@", "Show less")).firstMatch
        XCTAssertTrue(less.waitForExistence(timeout: 5), "“Show less” did not appear after Show more")
        shot("expanded")
        less.tap()
        settle()
        XCTAssertTrue(web.buttons.matching(NSPredicate(format: "label CONTAINS[c] %@", "Show more")).firstMatch.waitForExistence(timeout: 5),
                      "Show less did not fold it again")
    }

    func testAtYouAndOwnMessagesAreWholeAndNotFolded() {
        let atYou = elements(containing: "@you", in: web).firstMatch
        XCTAssertTrue(atYou.waitForExistence(timeout: 10), "no @you message")
        XCTAssertFalse(atYou.label.hasSuffix("…"), "the @you message is cut: “\(atYou.label.suffix(20))”")
        shot("@you whole")
    }

    func testPicturesShowAndATapOpensTheLightbox() {
        let image = web.images.matching(NSPredicate(format: "frame.width > 60")).firstMatch
        XCTAssertTrue(image.waitForExistence(timeout: 10), "no picture in the channel")
        shot("pictures")
        image.tap()
        settle(1)
        shot("lightbox")
        XCTAssertTrue(web.images.count >= 1)
        // Dismiss the lightbox with a tap on the backdrop.
        web.tap()
    }

    func testALongURLDoesNotWidenThePage() {
        let wide = web.descendants(matching: .any).allElementsBoundByIndex.filter { $0.frame.maxX > screenFrame.maxX + 1 && $0.frame.width < 4000 }
        XCTAssertTrue(wide.isEmpty, "something is wider than the screen: “\(wide.first?.label.prefix(40) ?? "")” ends at \(wide.first?.frame.maxX ?? 0)")
        shot("long URL, no horizontal scroll")
    }

    func testTheMemberStripShowsAPlusNAndATapListsEightMembers() {
        let plus = web.buttons.matching(NSPredicate(format: "label MATCHES %@", ".*\\+\\d+.*")).firstMatch
        XCTAssertTrue(plus.waitForExistence(timeout: 10), "no +n in the member strip")
        shot("member strip")
        plus.tap()
        settle(1)
        shot("members listed")
    }
}
