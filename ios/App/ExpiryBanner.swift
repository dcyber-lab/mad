import MadKit
import SwiftUI

/// How the installed build's provisioning profile stands (a free Apple ID's
/// lasts 7 days). The profile is absent in the simulator and in an App Store
/// build, so nothing shows there.
enum ProfileExpiry {
    static func current(now: Date = Date()) -> ProvisioningProfile.Expiry? {
        #if DEBUG
        // `-profileHoursLeft N` pretends the profile has N hours left, to see the notice.
        if let h = UserDefaults.standard.string(forKey: "profileHoursLeft").flatMap(Double.init) {
            return ProvisioningProfile.expiry(expiration: now.addingTimeInterval(h * 3600), now: now)
        }
        #endif
        guard let url = Bundle.main.url(forResource: "embedded", withExtension: "mobileprovision"),
              let data = try? Data(contentsOf: url),
              let date = ProvisioningProfile.expirationDate(in: data) else { return nil }
        return ProvisioningProfile.expiry(expiration: date, now: now)
    }
}

/// A thin banner for a build about to run out, with a way to put it away for
/// the day. It sits above the deck page, under the status bar.
struct ExpiryBar: View {
    let text: String
    let dismiss: () -> Void

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(text).font(.subheadline).frame(maxWidth: .infinity, alignment: .leading)
            Button(action: dismiss) {
                Image(systemName: "xmark").font(.subheadline.weight(.semibold))
            }
            .accessibilityLabel("关闭")
        }
        .padding(.horizontal, 16).padding(.vertical, 8)
        .foregroundStyle(.orange)
        // The tint runs up under the status bar; the text stays below it.
        .background(Color.orange.opacity(0.15).background(Color(.systemBackground)).ignoresSafeArea(edges: .top))
    }
}
