import XCTest
@testable import MadKit

final class AgentRemoveTests: XCTestCase {
    private let client = MadClient(mac: Mac(name: "t", baseURL: URL(string: "https://box.ts.net:7443")!, token: String(repeating: "a", count: 32)))

    func testRemovingAnAgentIsADeleteWithTheIdInTheQuery() throws {
        let r = client.removeAgentRequest(id: "a 1")
        XCTAssertEqual(r.httpMethod, "DELETE")
        XCTAssertEqual(r.url?.path, "/api/agents")
        XCTAssertEqual(URLComponents(url: r.url!, resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == "id" }?.value, "a 1")
        XCTAssertNil(r.httpBody, "the id is in the query, no body")
    }
}
