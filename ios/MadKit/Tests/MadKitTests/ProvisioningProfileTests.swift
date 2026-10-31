import XCTest
@testable import MadKit

final class ProvisioningProfileTests: XCTestCase {
    /// A profile as the app finds it: binary signing bytes around the plist.
    private func profile(expires: String = "2026-11-07T10:00:00Z", extra: String = "") -> Data {
        let plist = """
        <?xml version="1.0" encoding="UTF-8"?>
        <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
        <plist version="1.0"><dict>
          <key>Name</key><string>iOS Team Provisioning Profile: com.abcde12345.mad</string>
          <key>CreationDate</key><date>2026-10-31T10:00:00Z</date>
          <key>ExpirationDate</key><date>\(expires)</date>
          <key>TeamIdentifier</key><array><string>ABCDE12345</string></array>
          \(extra)
        </dict></plist>
        """
        var d = Data([0x30, 0x82, 0x0a, 0xff, 0x06, 0x09, 0x2a, 0x86])
        d.append(Data(plist.utf8))
        d.append(Data([0x00, 0xde, 0xad, 0xbe, 0xef]))
        return d
    }

    private let nov7 = ISO8601DateFormatter().date(from: "2026-11-07T10:00:00Z")!

    func testReadsTheExpirationDateOutOfTheSignedWrapper() {
        XCTAssertEqual(ProvisioningProfile.expirationDate(in: profile()), nov7)
    }

    func testNoPlistOrNoDateIsNil() {
        XCTAssertNil(ProvisioningProfile.expirationDate(in: Data([1, 2, 3])))
        XCTAssertNil(ProvisioningProfile.expirationDate(in: Data("<?xml no end".utf8)))
        let none = Data("<?xml version=\"1.0\"?><plist version=\"1.0\"><dict><key>Name</key><string>x</string></dict></plist>".utf8)
        XCTAssertNil(ProvisioningProfile.expirationDate(in: none))
    }

    func testDaysLeftRoundUp() {
        XCTAssertEqual(ProvisioningProfile.daysLeft(until: nov7, now: nov7.addingTimeInterval(-3600)), 1)
        XCTAssertEqual(ProvisioningProfile.daysLeft(until: nov7, now: nov7.addingTimeInterval(-86_400)), 1)
        XCTAssertEqual(ProvisioningProfile.daysLeft(until: nov7, now: nov7.addingTimeInterval(-86_401)), 2)
        XCTAssertEqual(ProvisioningProfile.daysLeft(until: nov7, now: nov7.addingTimeInterval(60)), 0)
    }

    func testNoticeOnlyWithTwoDaysOrFewerLeft() {
        func notice(daysBefore d: Double) -> String? {
            ProvisioningProfile.expiryNotice(expiration: nov7, now: nov7.addingTimeInterval(-d * 86_400))
        }
        XCTAssertNil(notice(daysBefore: 7))
        XCTAssertNil(notice(daysBefore: 3))
        XCTAssertNil(notice(daysBefore: 2.5))
        XCTAssertEqual(notice(daysBefore: 2), "This build expires in 2 days: run make ios-device on the Mac to renew.")
        XCTAssertEqual(notice(daysBefore: 1.5), "This build expires in 2 days: run make ios-device on the Mac to renew.")
        XCTAssertEqual(notice(daysBefore: 0.4), "This build expires in 1 day: run make ios-device on the Mac to renew.")
    }

    func testExpiredBuild() {
        XCTAssertEqual(ProvisioningProfile.expiryNotice(expiration: nov7, now: nov7.addingTimeInterval(10)),
                       "This build has expired: run make ios-device on the Mac to renew.")
    }

    func testAPaidAccountsYearLongProfileShowsNothing() {
        let year = nov7.addingTimeInterval(365 * 86_400)
        XCTAssertNil(ProvisioningProfile.expiryNotice(expiration: year, now: nov7))
    }
}
