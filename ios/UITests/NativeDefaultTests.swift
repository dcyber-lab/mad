import XCTest

/// The native screens are what the app opens; the fixture's edge-home scenario has the ios team and a few agents.
final class NativeDefaultTests: MadUITestCase {
    override func setUp() {
        super.setUp()
        // the expiry case launches for itself, with the profile about to run out
        if !name.contains("Expiry") { launch(native: true) }
    }

    /// The build running out is told on the native home too, and ✕ puts it away for the day.
    func testTheExpiryBannerShowsOverTheNativeHomeAndIsPutAway() {
        launch(native: true, extra: ["-profileHoursLeft", "30"])
        let banner = elements(containing: "这个版本 2 天后失效").firstMatch
        XCTAssertTrue(banner.waitForExistence(timeout: 40), "the expiry banner did not show over the native home")
        XCTAssertTrue(screenFrame.contains(banner.frame), "the banner is off the screen")
        shot("expiry banner over the native home")
        app.buttons["关闭"].firstMatch.tap()
        waitUntilGone(elements(containing: "这个版本").firstMatch, "the banner stayed after ✕")
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
        // the agent page's tabs (the terminal button is gone: 在终端打开 is in ⋯)
        XCTAssertTrue(element("对话").firstMatch.waitForExistence(timeout: 10), "the agent's conversation did not open")
        XCTAssertTrue(element("改动").firstMatch.exists, "the agent page has no 改动 tab")
        shot("the agent's conversation")
    }

    /// + → 新对话 is native: a box to write in, sending starts the agent and opens its conversation, and after the way
    /// back the new agent is in the home list (it was lost when the page did this in a sheet).
    func testNewChatIsNativeAndTheNewAgentIsInTheHomeAfterBack() {
        XCTAssertTrue(row(beginningWith: "ios").waitForExistence(timeout: 40), "the native home did not come up")
        tap("新建")
        tap("新对话")
        XCTAssertFalse(app.webViews.firstMatch.exists, "新对话 opened the page, not the native screen")
        let box = app.textViews["newchat.text"]
        XCTAssertTrue(box.waitForExistence(timeout: 10), "the native 新对话 has no box to write in")
        shot("the native 新对话")
        box.tap()
        box.typeText("Fixture started chat")
        let send = app.buttons["newchat.send"]
        XCTAssertTrue(send.isEnabled, "send is off with a message written")
        send.tap()
        XCTAssertTrue(element("对话").firstMatch.waitForExistence(timeout: 15), "sending did not open the new agent's conversation")
        shot("the new agent's conversation")
        tap("返回")
        let started = row(beginningWith: "Fixture started chat")
        XCTAssertTrue(started.waitForExistence(timeout: 15), "the new agent is not in the home list after the way back")
        shot("the new agent in the home list")
    }

    func testAMenuItemThatIsNotNativeYetOpensThePageInASheet() {
        tap("更多")
        let item = app.buttons["看板"].firstMatch
        XCTAssertTrue(item.waitForExistence(timeout: 5), "the ⋯ menu has no 看板")
        item.tap()
        XCTAssertTrue(app.webViews.firstMatch.waitForExistence(timeout: 15), "看板 did not open the page")
        XCTAssertTrue(element("完成").firstMatch.exists, "the sheet has no way to close")
        shot("看板 in a sheet")
        tap("完成")
        XCTAssertTrue(row(beginningWith: "ios").waitForExistence(timeout: 10), "closing the sheet did not show the home")
    }

    /// 用量 is native now: the person's row at the foot (and ⋯ → 用量) opens the Usage screen, with a way back.
    func testUsageIsANativeScreen() {
        XCTAssertTrue(row(beginningWith: "ios").waitForExistence(timeout: 40), "the native home did not come up")
        tap("更多")
        tap("用量")
        XCTAssertTrue(element("订阅额度").firstMatch.waitForExistence(timeout: 10), "用量 did not open the Usage screen")
        XCTAssertFalse(app.webViews.firstMatch.exists, "用量 opened the page, not the native screen")
        shot("the Usage screen")
        tap("返回")
        XCTAssertTrue(row(beginningWith: "ios").waitForExistence(timeout: 10), "back from Usage did not show the home")
    }

    func testThePlusMenuOffersNewChatChannelAndTeamAndAnItemOpensThePageOnItsDialog() {
        XCTAssertTrue(row(beginningWith: "ios").waitForExistence(timeout: 40), "the native home did not come up")
        tap("新建")
        XCTAssertTrue(app.buttons["新对话"].firstMatch.waitForExistence(timeout: 5), "the + menu has no 新对话")
        XCTAssertTrue(app.buttons["新团队"].firstMatch.exists, "the + menu has no 新团队")
        shot("the + menu")
        app.buttons["新群聊"].firstMatch.tap()
        XCTAssertTrue(app.webViews.firstMatch.waitForExistence(timeout: 15), "新群聊 did not open the page")
        XCTAssertTrue(element("完成").firstMatch.exists, "the sheet has no way to close")
        shot("新群聊 in a sheet")
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
