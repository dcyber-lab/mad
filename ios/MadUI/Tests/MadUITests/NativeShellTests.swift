import XCTest
@testable import MadUI

final class NativeShellTests: XCTestCase {
    func testSelectionsAreDistinct() {
        XCTAssertNotEqual(NativeShell.Selection.channel("a"), .agent("a"))
        XCTAssertEqual(NativeShell.Selection.channel("a"), .channel("a"))
    }
}
