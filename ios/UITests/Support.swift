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
        // The simulator is shared by every case: whatever a case did to it, it is upright again.
        XCUIDevice.shared.orientation = .portrait
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

    // MARK: Bounds

    /// The parts of the page that cross the screen's left or right edge, as one line each. A frame
    /// is the layout's, not what is drawn, so text that a clipped box cuts off at the screen's edge
    /// still shows here, where a screenshot hides it. Read in one snapshot of the whole tree.
    /// Scroll bars and the page's own full-size containers are not parts of the content.
    func partsPastTheScreenEdge(in root: XCUIElement? = nil, tolerance: CGFloat = 1) -> [String] {
        let window = screenFrame
        guard let tree = try? (root ?? web).snapshot() else { return ["the page's accessibility tree could not be read"] }
        var found: [String] = []
        func walk(_ s: XCUIElementSnapshot) {
            let f = s.frame
            let skipped = s.elementType == .webView || s.elementType == .scrollBar || s.label.contains("scroll bar")
            if !skipped, f.width > 0, f.height > 0, f.maxX > window.maxX + tolerance || f.minX < window.minX - tolerance {
                let name = s.label.isEmpty ? "(no label)" : "“" + String(s.label.prefix(60)) + (s.label.count > 60 ? "…" : "") + "”"
                found.append("\(Self.typeName(s.elementType)) \(name) spans x \(Int(f.minX))–\(Int(f.maxX)) on a screen \(Int(window.width)) wide")
            }
            s.children.forEach(walk)
        }
        walk(tree)
        return found
    }

    private static func typeName(_ t: XCUIElement.ElementType) -> String {
        switch t {
        case .staticText: return "text"
        case .button: return "button"
        case .link: return "link"
        case .image: return "image"
        case .textView, .textField: return "field"
        case .other: return "box"
        default: return "element(\(t.rawValue))"
        }
    }

    /// Turns the screen to landscape and waits for it, restoring portrait however the case ends.
    func rotateToLandscape() {
        XCUIDevice.shared.orientation = .landscapeLeft
        addTeardownBlock { XCUIDevice.shared.orientation = .portrait }
        wait("the screen did not turn to landscape") { self.screenFrame.width > self.screenFrame.height }
    }

    // MARK: Screenshots

    /// A named screenshot of the whole screen, kept for the report.
    func shot(_ name: String) {
        let a = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        a.name = name
        a.lifetime = .keepAlways
        add(a)
    }

    /// Waits for a state itself, not for time: an expectation on `condition` that fails the test
    /// with `message` if it does not hold within `timeout` seconds.
    @discardableResult
    func wait(_ message: @autoclosure () -> String, timeout: TimeInterval = 10, until condition: @escaping () -> Bool,
              file: StaticString = #filePath, line: UInt = #line) -> Bool {
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in condition() }, object: nil)
        let ok = XCTWaiter().wait(for: [expectation], timeout: timeout) == .completed
        if !ok { XCTFail(message(), file: file, line: line) }
        return ok
    }

    /// Waits until `element` is gone.
    func waitUntilGone(_ element: XCUIElement, _ message: @autoclosure () -> String, timeout: TimeInterval = 10,
                       file: StaticString = #filePath, line: UInt = #line) {
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: element)
        if XCTWaiter().wait(for: [expectation], timeout: timeout) != .completed { XCTFail(message(), file: file, line: line) }
    }
}
