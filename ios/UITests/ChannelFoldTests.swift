import XCTest

/// A long channel (`channel-fold` scenario): long messages as thin lines, no handoffs, results,
/// nudges or check-ins at all, whole @you and own messages, pictures with a lightbox, a long URL, a long quote,
/// the 群信息 panel, and the long-press menu.
final class ChannelFoldTests: MadUITestCase {
    /// The folded lines: buttons named by their text. Two of the scenario's messages fold, the long ones.
    private var lines: XCUIElementQuery {
        web.buttons.matching(NSPredicate(format: "label BEGINSWITH %@ OR label BEGINSWITH %@", "修改在这里", "命令"))
    }

    /// ⓘ, which opens 群信息 (the dialog it opens has the same label). It and the head's button
    /// say aria-haspopup, so WebKit makes them pop-up buttons, not buttons: any element will do.
    private var info: XCUIElement { element("群信息").firstMatch }
    /// The head: the channel's tile, its name and "空闲", one button that opens 群信息 too.
    private var head: XCUIElement { element("fold，空闲").firstMatch }

    override func setUp() {
        super.setUp()
        launch()
        waitForHome()
        // The scenario's one channel, “fold”, in 群聊.
        let row = web.buttons["fold"]
        XCTAssertTrue(row.waitForExistence(timeout: 10), "no “fold” row to open")
        row.tap()
        // A tap on a page that has only just come up is sometimes lost: once more, then it must have opened.
        if !element("发消息给 fold").firstMatch.waitForExistence(timeout: 10) { row.tap() }
        XCTAssertTrue(element("发消息给 fold").firstMatch.waitForExistence(timeout: 10), "the channel did not open (no composer)")
        wait("the channel's messages did not come") { self.lines.count >= 2 }
    }

    // MARK: Folded lines

    func testBookkeepingBetweenAgentsIsNotInTheChannel() {
        // The scenario has a handoff, a done report, a nudge, a check-in and a lapsed notice: none is drawn,
        // and no line is a pill ("→ builder", "完成").
        for gone in ["Do it on a branch", "Done: PR", "还没有 bus_done", "每小时检查", "已过期", "整理发布说明"] {
            XCTAssertEqual(elements(containing: gone).count, 0, "“\(gone)” is on the page")
        }
        XCTAssertEqual(web.buttons.matching(NSPredicate(format: "label BEGINSWITH %@ OR label BEGINSWITH %@", "→ builder", "完成")).count, 0,
                       "a handoff or result is still a line")
        // A result that @mentions the person is an ordinary bubble, whole, whatever its kind.
        XCTAssertTrue(elements(containing: "评审通过了，等你合并").firstMatch.exists, "the @you result is not shown")
        // mad's pause and resume notes stay, in Chinese.
        XCTAssertTrue(elements(containing: "群已暂停").firstMatch.exists, "the pause note is gone")
        XCTAssertTrue(elements(containing: "群已恢复").firstMatch.exists, "the resume note is gone")
        XCTAssertEqual(elements(containing: "Resumed by a person").count, 0, "the resume note is in English")
        shot("no bookkeeping")
        // The long messages are still lines.
        XCTAssertTrue(lines.firstMatch.exists, "no folded long message")
    }

    func testNoShowMoreAndNoMemberStripAnywhere() {
        for gone in ["Show more", "Show less", "Reply"] {
            XCTAssertEqual(element(gone).count, 0, "“\(gone)” is still on the page")
        }
        XCTAssertEqual(web.buttons.matching(NSPredicate(format: "label MATCHES %@", ".*\\+[0-9]+")).count, 0, "a +n member strip is still there")
    }

    func testAtYouAndOwnMessagesAreWholeAndNotFolded() {
        XCTAssertTrue(element("@you").firstMatch.exists, "no @you message")
        XCTAssertTrue(element("请看一下 #88，合并前需要你确认").firstMatch.exists, "the @you message is cut or missing")
        XCTAssertTrue(element("移动端我用了一下，字体太小了，群里也没法发图片").firstMatch.exists, "the person's first message is cut or missing")
        XCTAssertTrue(element("收到，谢谢大家").firstMatch.exists, "the person's second message is cut or missing")
        // The long agent messages are lines; neither @you nor the person's own messages are (they are whole above).
        shot("@you and own messages whole")
    }

    // MARK: Quiet messages

    func testNoSequenceNumbersOrClocksOnMessages() {
        XCTAssertEqual(web.staticTexts.matching(NSPredicate(format: "label MATCHES %@", "#[0-9]+ · .*")).count, 0, "a #seq · time still shows on a message")
        XCTAssertTrue(elements(containing: "今天").firstMatch.exists || elements(containing: "昨天").firstMatch.exists
                      || elements(containing: "月").firstMatch.exists, "no centred time line")
    }

    // MARK: Pictures

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

    // MARK: Nothing wider than the screen

    func testALongURLDoesNotWidenThePage() {
        let link = web.links.firstMatch
        XCTAssertTrue(link.waitForExistence(timeout: 10), "no link in the channel")
        XCTAssertLessThanOrEqual(link.frame.maxX, screenFrame.maxX, "the long URL runs past the screen: ends at \(link.frame.maxX)")
        let hbar = web.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH %@", "Horizontal scroll bar")).firstMatch
        if hbar.exists { XCTAssertTrue(hbar.label.contains("1 page"), "the page scrolls sideways: “\(hbar.label)”") }
        shot("long URL, no sideways scroll")
    }

    func testLongMessagesAndTheQuoteStayInsideTheScreen() {
        // The long message of the person, its answer (which quotes it in one line), and the long
        // unbroken Chinese one: every text of the log ends before the screen does.
        let long = web.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@ OR label BEGINSWITH %@ OR label BEGINSWITH %@",
                                                        "我看了一下这条消息", "这是一整段没有任何空格", "好的，我让 builder"))
        wait("the long messages did not come") { long.count >= 3 }
        for t in long.allElementsBoundByIndex {
            XCTAssertLessThanOrEqual(t.frame.maxX, screenFrame.maxX, "“\(t.label.prefix(20))…” runs past the screen: ends at \(t.frame.maxX)")
        }
        // The quote is one line, ellipsized: it is a button that names the person and starts with their words.
        let quote = web.buttons.matching(NSPredicate(format: "label CONTAINS %@ AND label CONTAINS %@", "你", "我看了一下这条消息")).firstMatch
        XCTAssertTrue(quote.waitForExistence(timeout: 5), "the reply has no quote")
        XCTAssertLessThanOrEqual(quote.frame.maxX, screenFrame.maxX, "the quote runs past the screen: ends at \(quote.frame.maxX)")
        XCTAssertLessThanOrEqual(quote.frame.height, 40, "the quote is more than one line: \(quote.frame.height) high")
        shot("long messages and a quote")
    }

    // MARK: The head and 群信息

    func testTheHeadIsOneLineAndTheInfoButtonOpensGroupInfoWithEightMembers() {
        XCTAssertTrue(head.waitForExistence(timeout: 5), "the head has no name and “空闲”: \(web.descendants(matching: .any).allElementsBoundByIndex.prefix(60).map { $0.label }.filter { !$0.isEmpty })")
        XCTAssertTrue(info.exists, "no ⓘ 群信息 button")
        shot("one-line head")
        info.tap()
        let members = element("成员 · 8").firstMatch
        XCTAssertTrue(members.waitForExistence(timeout: 10), "群信息 did not open with “成员 · 8”")
        shot("群信息")
        for name in ["architect", "builder", "reviewer", "designer", "tester", "docs", "ops", "kim"] {
            let row = web.buttons.matching(NSPredicate(format: "label CONTAINS %@ AND label CONTAINS %@", name, "空闲")).firstMatch
            XCTAssertTrue(row.exists, "群信息 has no member row for “\(name)”")
        }
        XCTAssertTrue(elements(containing: "负责人").firstMatch.exists, "the lead has no 负责人 tag")
        for item in ["任务板", "图片和文件", "关闭"] {
            XCTAssertTrue(web.buttons.matching(NSPredicate(format: "label CONTAINS %@", item)).firstMatch.exists, "群信息 has no “\(item)”")
        }
        XCTAssertTrue(elements(containing: "2 进行中").firstMatch.exists, "任务板 does not count this channel's two tasks in progress")
        // A swipe to the right closes it.
        web.swipeRight()
        waitUntilGone(members, "a swipe to the right did not close 群信息")
        XCTAssertTrue(info.exists, "ⓘ is gone after closing")
        shot("closed by a swipe")
    }

    func testTheHeadOpensGroupInfoAndCloseAndThePicturesPage() {
        head.tap()
        XCTAssertTrue(element("成员 · 8").firstMatch.waitForExistence(timeout: 10), "tapping the name did not open 群信息")
        web.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "图片和文件")).firstMatch.tap()
        let grid = web.buttons.matching(NSPredicate(format: "label == %@", "图片，来自 reviewer"))
        // The panel's grid has the two pictures (the page behind it is hidden from the accessibility tree
        // while the dialog is modal, so they are not counted twice).
        wait("the pictures page did not show its two pictures: \(grid.count)") { grid.count >= 2 }
        shot("图片和文件")
        web.buttons["关闭"].firstMatch.tap()
        waitUntilGone(element("成员 · 8").firstMatch, "关闭 did not close 群信息")
    }

    // MARK: Reply

    func testALongPressOffersReplyAndCopyAndReplyQuotesIt() {
        // Each message has a 回复 button (clipped on a touch screen, there for a screen reader).
        XCTAssertGreaterThanOrEqual(web.buttons.matching(NSPredicate(format: "label == %@", "回复")).count, 8, "the messages have no 回复 buttons")
        let said = element("两张截图：左边是修改前，右边是修改后。").firstMatch
        XCTAssertTrue(said.waitForExistence(timeout: 5), "no message to press")
        said.press(forDuration: 1.0)
        XCTAssertTrue(element("复制").firstMatch.waitForExistence(timeout: 5), "a long press did not open a menu with 复制")
        shot("long-press menu")
        // The menu's 回复 is the last of those named so: the menu is the page's last element.
        let replies = element("回复")
        replies.element(boundBy: replies.count - 1).tap()
        XCTAssertTrue(elements(containing: "回复 reviewer").firstMatch.waitForExistence(timeout: 5), "回复 did not start a reply to reviewer")
        shot("replying")
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
