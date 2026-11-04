import XCTest
@testable import MadKit

final class PhoneTests: XCTestCase {
    func testStatusOff() throws {
        let s = try JSONDecoder().decode(PhoneStatus.self, from: Data(#"{"on":false,"via":"tailscale","host":"m.ts.net"}"#.utf8))
        XCTAssertFalse(s.on)
        XCTAssertNil(s.url)
        XCTAssertNil(s.problem)
    }

    func testStatusProblem() throws {
        let s = try JSONDecoder().decode(PhoneStatus.self, from: Data(#"{"on":false,"problem":"Tailscale is not running"}"#.utf8))
        XCTAssertEqual(s.problem, "Tailscale is not running")
    }
}
