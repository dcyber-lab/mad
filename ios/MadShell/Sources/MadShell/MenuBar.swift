#if os(macOS)
import MadUI
import SwiftUI

/// The sidebar's menu (用量, 看板, 手机和已配对设备 ...) in the Mac's menu bar, as 前往. Since v5 the sidebar has no ⋯:
/// the menu was only a right click on the person's row, which no one finds.
public struct MadMenuCommands: Commands {
    @FocusedValue(\.madHomeMenu) private var menu

    public init() {}

    public var body: some Commands {
        CommandMenu(L("前往")) {
            ForEach(Array(HomeMenuItem.sidebar.enumerated()), id: \.offset) { i, group in
                if i > 0 { Divider() }
                ForEach(group, id: \.self) { item in
                    Button(item.title) { menu?(item) }.disabled(menu == nil)
                }
            }
        }
    }
}

struct HomeMenuKey: FocusedValueKey {
    typealias Value = (HomeMenuItem) -> Void
}

extension FocusedValues {
    /// What the window's sidebar menu does with an item, for the menu bar.
    var madHomeMenu: HomeMenuKey.Value? {
        get { self[HomeMenuKey.self] }
        set { self[HomeMenuKey.self] = newValue }
    }
}
#endif
