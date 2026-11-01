import XCTest

/// Pairing with the fixture server's `pairing` scenario: aaaa… is taken, bbbb… refused.
final class PairingTests: MadUITestCase {
    func testAGoodCodePairs() {
        launch(link: "\(fixtureURL)/#pair=\(Self.goodCode)", open: false)
        let row = app.cells.firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 20), "the Mac was not added")
        XCTAssertFalse(app.staticTexts["还没配对完"].exists)
        XCTAssertFalse(app.alerts.firstMatch.exists, "an error came up")
        shot("paired: the Mac is on the list")
    }

    func testARefusedCodeAsksForANewQR() {
        launch(link: "\(fixtureURL)/#pair=\(Self.badCode)", open: false)
        let alert = app.alerts["配对失败"]
        XCTAssertTrue(alert.waitForExistence(timeout: 20), "no “Cannot pair” alert")
        XCTAssertTrue(alert.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "scan a new QR")).firstMatch.exists,
                      "the alert does not say to scan a new QR code")
        shot("refused: scan a new QR")
        alert.buttons["好"].tap()
        XCTAssertEqual(app.cells.count, 0, "a refused pairing left a row")
        XCTAssertFalse(app.staticTexts["还没配对完"].exists, "a refused pairing stayed pending")
    }
}

extension MadUITestCase {
    static let flakyCode = String(repeating: "c", count: 32)
}

/// ccc… answers 503 the first time and 200 after: the Retry path, without stopping the server.
final class PairingRetryTests: MadUITestCase {
    func testA503KeepsThePairingAndRetryPairs() {
        launch(link: "\(fixtureURL)/#pair=\(Self.flakyCode)", open: false)
        let alert = app.alerts["配对失败"]
        XCTAssertTrue(alert.waitForExistence(timeout: 20), "no alert for a 503")
        XCTAssertTrue(alert.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "503")).firstMatch.exists, "the alert does not say 503")
        shot("503: the pairing is kept")
        alert.buttons["好"].tap()
        XCTAssertTrue(app.staticTexts["还没配对完"].waitForExistence(timeout: 5), "no “还没配对完” row")
        XCTAssertTrue(app.buttons["重试"].exists, "no 重试 button")
        shot("Pairing not finished, with Retry")
        app.buttons["重试"].tap()
        XCTAssertTrue(app.cells.firstMatch.waitForExistence(timeout: 20), "Retry did not pair")
        XCTAssertFalse(app.staticTexts["还没配对完"].exists, "still pending after a good Retry")
        shot("Retry paired")
    }
}

/// The lost-answer path, in two runs with the server's state changed between them by
/// the harness: the server is down in the first, back in the second.
final class PairingOfflineTests: MadUITestCase {
    func testA_ServerDownKeepsThePairingWithRetry() {
        launch(link: "\(fixtureURL)/#pair=\(Self.goodCode)", open: false)
        let alert = app.alerts["配对失败"]
        XCTAssertTrue(alert.waitForExistence(timeout: 30), "no alert for an unreachable Mac")
        XCTAssertTrue(alert.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "pairing is kept")).firstMatch.exists)
        shot("server down: the pairing is kept")
        alert.buttons["好"].tap()
        XCTAssertTrue(app.staticTexts["还没配对完"].waitForExistence(timeout: 5), "no “还没配对完” row")
        XCTAssertTrue(app.buttons["重试"].exists, "no 重试 button")
        shot("Pairing not finished, with Retry")
    }

    func testB_RetryAfterTheServerIsBackPairs() {
        relaunch()
        XCTAssertTrue(app.staticTexts["还没配对完"].waitForExistence(timeout: 10), "the pending pairing did not survive a relaunch")
        app.buttons["重试"].tap()
        XCTAssertTrue(app.cells.firstMatch.waitForExistence(timeout: 20), "Retry did not pair")
        XCTAssertFalse(app.staticTexts["还没配对完"].exists, "still pending after a good Retry")
        shot("Retry paired")
    }
}
