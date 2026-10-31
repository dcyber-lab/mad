import XCTest
@testable import MadKit

private let token = "0123456789abcdef0123456789abcdef"

private func client() -> MadClient {
    MadClient(mac: Mac(name: "t", baseURL: URL(string: "https://box.ts.net:7443")!, token: token))
}

private func http(_ code: Int) -> URLResponse {
    HTTPURLResponse(url: URL(string: "https://x")!, statusCode: code, httpVersion: nil, headerFields: nil)!
}

private func query(_ r: URLRequest, _ name: String) -> String? {
    URLComponents(url: r.url!, resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == name }?.value
}

private func body(_ r: URLRequest) throws -> [String: Any] {
    try XCTUnwrap(JSONSerialization.jsonObject(with: try XCTUnwrap(r.httpBody)) as? [String: Any])
}

final class ClientBusTests: XCTestCase {
    func testMessagesRequestAsksBySince() {
        let r = client().messagesRequest(channel: "fold", since: 12)
        XCTAssertEqual(r.url?.path, "/api/bus/messages")
        XCTAssertEqual(query(r, "channel"), "fold")
        XCTAssertEqual(query(r, "since"), "12")
        XCTAssertEqual(r.value(forHTTPHeaderField: "Authorization"), "Bearer \(token)")
        XCTAssertFalse(r.url!.absoluteString.contains(token), "the token is in the header, not the URL")
    }

    func testPostBodyHasTextReplyAndPictures() throws {
        let png = Data([0x89, 0x50, 0x4e, 0x47])
        let r = client().postRequest(channel: "fold", text: "hello", replyTo: 9, images: [PostImage(mime: "image/png", data: png)])
        XCTAssertEqual(r.httpMethod, "POST")
        XCTAssertEqual(r.url?.path, "/api/bus/post")
        XCTAssertEqual(r.value(forHTTPHeaderField: "Content-Type"), "application/json")
        let b = try body(r)
        XCTAssertEqual(b["channel"] as? String, "fold")
        XCTAssertEqual(b["text"] as? String, "hello")
        XCTAssertEqual(b["reply_to"] as? Int, 9)
        let images = try XCTUnwrap(b["images"] as? [[String: String]])
        XCTAssertEqual(images, [["mime": "image/png", "data": png.base64EncodedString()]])
    }

    func testPostWithoutAReplyOrPicturesSendsNeither() throws {
        let b = try body(client().postRequest(channel: "c", text: "x", replyTo: nil, images: []))
        XCTAssertNil(b["reply_to"])
        XCTAssertNil(b["images"])
        XCTAssertNil(try body(client().postRequest(channel: "c", text: "x", replyTo: 0, images: []))["reply_to"])
    }

    func testDMReadAndImageRequests() throws {
        let c = client()
        let dm = try body(c.dmRequest(to: "kim", text: "hi", images: []))
        XCTAssertEqual(dm["to"] as? String, "kim")
        XCTAssertEqual(c.dmRequest(to: "kim", text: "hi", images: []).url?.path, "/api/bus/dm")
        let read = c.readRequest(channel: "fold", seq: 13)
        XCTAssertEqual(read.url?.path, "/api/bus/read")
        XCTAssertEqual(try body(read)["seq"] as? Int, 13)
        let img = c.imageRequest(id: "abc")
        XCTAssertEqual(img.url?.path, "/api/bus/image")
        XCTAssertEqual(query(img, "id"), "abc")
        XCTAssertEqual(img.value(forHTTPHeaderField: "Authorization"), "Bearer \(token)")
    }

    func testBusErrorsKeepTheServersWordsAndOnlyForbiddenIsARefusedToken() {
        XCTAssertThrowsError(try MadClient.checkBus(Data("forbidden\n".utf8), http(403), [200])) {
            XCTAssertEqual($0 as? ConnectionError, .refused)
        }
        XCTAssertThrowsError(try MadClient.checkBus(Data("that DM is not yours".utf8), http(403), [200])) {
            XCTAssertEqual($0 as? ConnectionError, .unexpected("that DM is not yours"))
        }
        XCTAssertThrowsError(try MadClient.checkBus(Data("no such channel".utf8), http(404), [200])) {
            XCTAssertEqual($0 as? ConnectionError, .unexpected("no such channel"))
        }
        XCTAssertThrowsError(try MadClient.checkBus(Data("the channel is paused".utf8), http(409), [200])) {
            XCTAssertEqual($0 as? ConnectionError, .conflict("the channel is paused"))
        }
        XCTAssertNoThrow(try MadClient.checkBus(Data(), http(200), [200]))
    }
}
