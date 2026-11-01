import XCTest

/// The native screens are what the app opens; the fixture's edge-home scenario has the ios team and a few agents.
final class NativeDefaultTests: MadUITestCase {
    override func setUp() {
        super.setUp()
        launch(native: true)
    }

    private func row(beginningWith text: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH %@", text)).firstMatch
    }

    func testNativeHomeThenTheTeamChannelThenAnAgentConversation() {
        XCTAssertFalse(app.webViews.firstMatch.exists, "the app opened the web page, not the native home")
        let team = row(beginningWith: "ios")
        XCTAssertTrue(team.waitForExistence(timeout: 40), "the native home did not come up: no ios team row")
        shot("native home")
        team.tap()
        XCTAssertTrue(element("群信息").firstMatch.waitForExistence(timeout: 10), "the ios team did not open its channel")
        shot("the ios channel")
        tap("返回")
        let agent = row(beginningWith: "Sidebar token 统计")
        XCTAssertTrue(agent.waitForExistence(timeout: 10), "back from the channel did not show the home")
        agent.tap()
        XCTAssertTrue(element("在网页里打开终端/改动").firstMatch.waitForExistence(timeout: 10), "the agent's conversation did not open")
        shot("the agent's conversation")
    }

    func testAMenuItemThatIsNotNativeYetOpensThePageInASheet() {
        tap("更多")
        let item = app.buttons["用量"].firstMatch
        XCTAssertTrue(item.waitForExistence(timeout: 5), "the ⋯ menu has no 用量")
        item.tap()
        XCTAssertTrue(app.webViews.firstMatch.waitForExistence(timeout: 15), "用量 did not open the page")
        XCTAssertTrue(element("完成").firstMatch.exists, "the sheet has no way to close")
        shot("用量 in a sheet")
        tap("完成")
        XCTAssertTrue(row(beginningWith: "ios").waitForExistence(timeout: 10), "closing the sheet did not show the home")
    }

    func testSwitchMacIsReachableAndLongPressChoosesTheWebVersion() {
        XCTAssertTrue(row(beginningWith: "ios").waitForExistence(timeout: 40))
        XCTAssertGreaterThanOrEqual(elements(containing: "switch Mac").count, 1, "no way to switch Mac on the home")
        tap("更多")
        tap("切换 Mac")
        let mac = app.cells.firstMatch
        XCTAssertTrue(mac.waitForExistence(timeout: 10), "switching Mac did not show the Macs list")
        mac.press(forDuration: 1.2)
        tap("使用网页版")
        mac.tap()
        waitForHome() // the web page's own title ▾
        shot("the web version, chosen for this Mac")
    }
}
