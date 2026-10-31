import XCTest

/// The app's own behaviour around the page: the expiry banner, and the way back to the Macs list.
final class AppTests: MadUITestCase {
    func testExpiryBannerShowsAndIsPutAwayForTheDay() {
        launch(extra: ["-profileHoursLeft", "30"])
        waitForHome()
        let banner = elements(containing: "这个版本 2 天后失效").firstMatch
        XCTAssertTrue(banner.waitForExistence(timeout: 10), "the expiry banner did not show")
        XCTAssertTrue(banner.frame.minY >= 0 && screenFrame.contains(banner.frame), "the banner is off the screen")
        shot("expiry banner over the page")

        let close = app.buttons["关闭"].firstMatch
        XCTAssertTrue(close.exists, "the banner has no ✕")
        close.tap()
        waitUntilGone(elements(containing: "这个版本").firstMatch, "the banner stayed after ✕")
        shot("banner put away")

        // Put away for the day: it stays away when the Mac opens again.
        relaunch(extra: ["-profileHoursLeft", "30"])
        waitForHome() // the page is up, and the banner is decided when the Mac opens, before that
        XCTAssertFalse(elements(containing: "这个版本").firstMatch.exists, "the banner came back the same day")
    }

    func testSwitchMacGoesBackToTheListAndTheMacOpensAgain() {
        launch()
        let caret = waitForHome()
        shot("the Mac open")
        XCTAssertTrue(caret.isHittable, "▾ 切换 Mac is not hittable")
        caret.tap()
        // The Macs list: its title, and a row for the Mac.
        XCTAssertTrue(app.navigationBars["mad"].waitForExistence(timeout: 10) || app.staticTexts["mad"].waitForExistence(timeout: 10),
                      "did not get back to the Macs list")
        XCTAssertTrue(app.cells.firstMatch.waitForExistence(timeout: 10), "the Macs list has no Mac")
        shot("back on the Macs list")

        app.cells.firstMatch.tap()
        waitForHome()
        shot("the Mac open again")
    }

    func testLastUsedMacOpensOnLaunch() {
        launch()
        waitForHome()
        relaunch()
        waitForHome()
        shot("relaunch opens the last Mac")
    }
}
