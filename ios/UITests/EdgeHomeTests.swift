import XCTest

/// The home page's edge cases, against the fixture server's `edge-home` scenario.
final class EdgeHomeTests: MadUITestCase {
    override func setUp() {
        super.setUp()
        launch()
        waitForHome()
    }

    func testLongMacNameEllipsizesAndTheCaretStaysHittableOnScreen() {
        let caret = element("切换 Mac").firstMatch
        XCTAssertTrue(caret.isHittable, "▾ 切换 Mac is not hittable")
        XCTAssertTrue(screenFrame.contains(caret.frame), "▾ is outside the screen: \(caret.frame) in \(screenFrame)")
        // The name is the longest text in the header; it must end before the ▾ starts.
        let header = web.staticTexts.allElementsBoundByIndex.filter { $0.frame.minY < 200 && $0.label.count >= 40 }
        XCTAssertFalse(header.isEmpty, "no long Mac name in the header")
        if let name = header.first { XCTAssertLessThanOrEqual(name.frame.maxX, caret.frame.minX + 1, "the name runs under the ▾") }
        shot("long Mac name, ▾ stays")
    }

    /// How many images named "needs you" lie in a row: the red dot, and the character or tile
    /// when it says so too.
    private func needsYouImages(in row: XCUIElement) -> Int {
        element("needs you").allElementsBoundByIndex.filter { row.frame.contains(CGPoint(x: $0.frame.midX, y: $0.frame.midY)) }.count
    }

    func testTheRowsWithAnAtYouShowANeedsYouDotAndTheOthersDoNot() {
        // ios and five: one image, the dot (their tiles say "working", since a member works); the
        // needs-you agent: two (the character says so, and the dot); the others: none.
        for (name, want) in [("ios", 1), ("five", 1), ("给我绘制一个表格", 2), ("shots", 0), ("pair", 0), ("solo", 0)] {
            let row = element(name).firstMatch
            XCTAssertTrue(row.waitForExistence(timeout: 10), "no row named \(name)")
            XCTAssertEqual(needsYouImages(in: row), want, "the \(name) row has \(needsYouImages(in: row)) “needs you” image(s), expected \(want)")
        }
        shot("the rows with the red dot")
    }

    func testChatsAreOrderedRedDotsFirstThenNewest() {
        let names = ["ios", "five", "shots", "pair", "solo"]
        let ys = names.map { element($0).firstMatch.frame.minY }
        XCTAssertEqual(ys, ys.sorted(), "the chat order is not ios, five, shots, pair, solo: \(zip(names, ys).map { "\($0) \(Int($1))" })")
    }

    func testTilesOfOneTwoAndFiveMembersAreDrawn() {
        // The tile sits at the row's left: something is drawn there for each of the three.
        for name in ["solo", "pair", "five"] {
            let row = element(name).firstMatch
            XCTAssertTrue(row.exists, "no row named \(name)")
            let tile = web.images.allElementsBoundByIndex.filter { $0.frame.minX < 100 && $0.frame.midY > row.frame.minY && $0.frame.midY < row.frame.maxY && $0.frame.width >= 30 }
            XCTAssertFalse(tile.isEmpty, "the \(name) row has no tile at its left")
        }
        shot("tiles of 1, 2 and 5 members")
    }

    func testCharactersHaveTheirStatesAsLabels() {
        for state in ["working", "needs you", "stopped"] {
            XCTAssertTrue(element(state).firstMatch.waitForExistence(timeout: 5), "no character labelled “\(state)”")
        }
        shot("working, needs-you and stopped characters")
    }

    func testAnUntitledAgentReadsKindAndTheStartOfItsId() {
        let untitled = web.descendants(matching: .any).matching(NSPredicate(format: "label MATCHES %@", "[a-z]+ [0-9a-z]{8}")).firstMatch
        XCTAssertTrue(untitled.waitForExistence(timeout: 5), "no untitled agent reads “kind xxxxxxxx”")
        XCTAssertTrue(element("codex unti0001").firstMatch.exists, "the untitled agent is not “codex unti0001”")
    }

    func testPicturesOnlyChannelPreviewsAsImage() {
        XCTAssertTrue(elements(containing: "发布助手: [图片]").firstMatch.waitForExistence(timeout: 5), "no “发布助手: [图片]” preview on the shots row")
    }

    func testALongCJKLineStaysOnOneLine() {
        // The preview under the 长文本测试 row is far longer than the screen is wide.
        let line = web.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "这是一段很长的中文回复")).firstMatch
        XCTAssertTrue(line.waitForExistence(timeout: 5), "no long CJK preview line")
        XCTAssertLessThanOrEqual(line.frame.height, 24, "the long line wrapped: it is \(line.frame.height) high")
        XCTAssertLessThanOrEqual(line.frame.maxX, screenFrame.maxX - 8, "the long line runs to the screen's edge: ends at \(line.frame.maxX)")
        XCTAssertTrue(element("长文本测试").firstMatch.exists, "the row for the long line is gone")
    }

    func testSearchFindsTheTeamRow() {
        tap("搜索")
        // The page focuses the field itself, but a tap that comes as the keyboard rises can lose that:
        // the field is tapped too, so it has the keyboard before the typing.
        let field = web.searchFields.firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 10), "the search field did not open")
        field.tap()
        app.typeText("builder")
        waitUntilGone(element("solo").firstMatch, "searching “builder” still lists the solo chat")
        XCTAssertTrue(element("ios").firstMatch.exists, "searching “builder” did not find the ios team row")
        shot("search “builder” finds the team")
    }

    func testTheMoreMenuListsItsItemsAndNotTheFullPage() {
        tap("更多")
        for item in ["用量", "看板", "技能", "手机和已配对设备", "添加项目"] {
            XCTAssertTrue(element(item).firstMatch.waitForExistence(timeout: 5), "the ⋯ menu has no “\(item)”")
        }
        XCTAssertTrue(elements(containing: "主题").firstMatch.exists, "the ⋯ menu has no 主题")
        XCTAssertGreaterThanOrEqual(element("切换 Mac").count, 2, "the ⋯ menu has no 切换 Mac (only the title's ▾)")
        XCTAssertFalse(element("显示完整页面").firstMatch.exists, "the ⋯ menu still offers 显示完整页面")
        shot("the ⋯ menu")
    }

    func testDarkMode() {
        relaunch(extra: ["-AppleInterfaceStyle", "Dark"])
        waitForHome()
        shot("dark mode")
    }

    func testLandscape() {
        XCUIDevice.shared.orientation = .landscapeLeft
        // Upright again whatever happens below, so a failure cannot leave the shared simulator on its side.
        addTeardownBlock { XCUIDevice.shared.orientation = .portrait }
        wait("the screen did not turn to landscape") { self.screenFrame.width > self.screenFrame.height }
        let caret = element("切换 Mac").firstMatch
        XCTAssertTrue(caret.waitForExistence(timeout: 10))
        XCTAssertTrue(screenFrame.contains(caret.frame), "▾ is off the screen in landscape")
        shot("landscape")
    }

    // The same check as in a channel: no name or preview line of a row may run past the screen's edge.

    func testNoPartOfTheListCrossesTheScreenEdgeInPortrait() {
        let past = partsPastTheScreenEdge()
        XCTAssertTrue(past.isEmpty, "\(past.count) part(s) of the list cross the screen edge in portrait:\n" + past.prefix(12).joined(separator: "\n"))
    }

    func testNoPartOfTheListCrossesTheScreenEdgeInLandscape() {
        rotateToLandscape()
        XCTAssertTrue(element("切换 Mac").firstMatch.waitForExistence(timeout: 10))
        shot("list, landscape")
        let past = partsPastTheScreenEdge()
        XCTAssertTrue(past.isEmpty, "\(past.count) part(s) of the list cross the screen edge in landscape:\n" + past.prefix(12).joined(separator: "\n"))
    }
}
