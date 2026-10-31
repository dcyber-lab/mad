import XCTest

/// What every case shares: launching the app against the fixture server, finding
/// things in the page by their accessibility label, and attaching named screenshots
/// for the report.
class MadUITestCase: XCTestCase {
    var app: XCUIApplication!

    /// A token the fixture server takes (it accepts any); 32 hex digits, as pairing needs.
    static let token = "0123456789abcdef0123456789abcdef"
    static let goodCode = String(repeating: "a", count: 32)
    static let badCode = String(repeating: "b", count: 32)

    /// The fixture server's address, from the harness (TEST_RUNNER_MAD_FIXTURE_URL).
    var fixtureURL: String {
        ProcessInfo.processInfo.environment["MAD_FIXTURE_URL"] ?? "http://127.0.0.1:1"
    }

    override func setUp() {
        super.setUp()
        continueAfterFailure = false
    }

    override func tearDown() {
        if let run = testRun, run.totalFailureCount > 0, app != nil {
            shot("on failure")
        }
        super.tearDown()
    }

    // MARK: Launching

    /// Launches the app: forgets every Mac first, pairs with the fixture server (a
    /// token link, or `link` as given), and opens the Mac.
    func launch(link: String? = nil, open: Bool = true, reset: Bool = true, extra: [String] = []) {
        app = XCUIApplication()
        var args: [String] = []
        if reset { args += ["-resetStore", "YES"] }
        args += ["-pairLink", link ?? "\(fixtureURL)/#token=\(Self.token)"]
        if open { args += ["-openMac", "YES"] }
        app.launchArguments = args + extra
        app.launch()
    }

    /// Launches again without forgetting or pairing anything (the Mac last used opens).
    func relaunch(extra: [String] = []) {
        app?.terminate()
        app = XCUIApplication()
        app.launchArguments = extra
        app.launch()
    }

    // MARK: Finding

    var web: XCUIElement { app.webViews.firstMatch }

    /// An element of the page (or of the app) by its exact accessibility label.
    func element(_ label: String, in root: XCUIElement? = nil) -> XCUIElementQuery {
        (root ?? app).descendants(matching: .any).matching(NSPredicate(format: "label == %@", label))
    }

    /// Elements whose label contains `text`.
    func elements(containing text: String, in root: XCUIElement? = nil) -> XCUIElementQuery {
        (root ?? app).descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", text))
    }

    /// Waits for the page's first screen: the title's ▾ is how the page says it is up.
    @discardableResult
    func waitForHome(timeout: TimeInterval = 40) -> XCUIElement {
        let caret = element("切换 Mac").firstMatch
        XCTAssertTrue(caret.waitForExistence(timeout: timeout), "the page did not come up: no 切换 Mac ▾")
        return caret
    }

    func tap(_ label: String, timeout: TimeInterval = 10, file: StaticString = #filePath, line: UInt = #line) {
        let e = element(label).firstMatch
        XCTAssertTrue(e.waitForExistence(timeout: timeout), "no “\(label)” to tap", file: file, line: line)
        e.tap()
    }

    func exists(_ label: String, timeout: TimeInterval = 5) -> Bool {
        element(label).firstMatch.waitForExistence(timeout: timeout)
    }

    var screenFrame: CGRect { app.windows.firstMatch.frame }

    // MARK: Screenshots

    /// A named screenshot of the whole screen, kept for the report.
    func shot(_ name: String) {
        let a = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        a.name = name
        a.lifetime = .keepAlways
        add(a)
    }

    /// Sleeps a moment for an animation or a render to finish.
    func settle(_ seconds: TimeInterval = 1) { Thread.sleep(forTimeInterval: seconds) }
}
