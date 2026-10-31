import SwiftUI

#if DEBUG
/// A model like the fixture's edge-home scenario (internal/desktop/fixture/testdata/edge-home.json):
/// the long machine name, tiles of 1, 2, 3 and 5, "[图片]", an untitled agent, long CJK, emoji, a stopped
/// agent, red dots. HomeSampleTests checks it against that file, so it cannot drift.
public enum SampleHome {
    static func m(_ id: String, _ name: String, _ state: CharacterState = .idle, _ preview: String = "") -> HomeMember {
        HomeMember(id: id, kind: "claude", state: state, name: name, preview: preview)
    }

    static let arch = m("team0001-arch", "architect", .idle, "第 3 条也上线了，三条全部完成。")
    static let builder = m("team0001-bld0", "builder", .working, "在写代码")
    static let reviewer = m("team0001-rev0", "reviewer", .idle, "评审 #88：通过")
    static let work = m("work0001-aaaa", "Sidebar token 统计", .working, "正在跑测试…")
    static let need = m("need0001-bbbb", "给我绘制一个表格", .needsYou, "想运行一条命令，等你确认")
    static let cjk = m("cjk00001-ffff", "长文本测试", .idle, "收到")
    static let notes = m("idle0001-dddd", "notes", .idle, "好的")
    static let emoji = m("emoji001-gggg", "🚀 发布助手", .idle, "[图片]")

    public static let model = HomeModel(
        machine: "Heng Du’s MacBook Pro (the 16-inch one on the desk at home).",
        sections: [
            HomeSection(id: "chats", title: "群聊", kind: .chats, rows: [
                HomeRow(id: "ios", kind: .channel, name: "ios", preview: "architect: 第 3 条也上线了，三条全部完成。",
                        state: .working, needsYou: true, members: [arch, builder, reviewer]),
                HomeRow(id: "solo", kind: .channel, name: "solo", preview: "notes: 好的", members: [notes]),
                HomeRow(id: "pair", kind: .channel, name: "pair", preview: "长文本测试: 收到", state: .working, members: [work, cjk]),
                HomeRow(id: "five", kind: .channel, name: "five", preview: "kim: 五个成员的群", needsYou: true, members: [arch, builder, reviewer, work, need]),
                HomeRow(id: "shots", kind: .channel, name: "shots", preview: "🚀 发布助手: [图片]", members: [emoji]),
            ]),
            HomeSection(id: "mad", title: "mad", kind: .project(name: "mad"), rows: [
                HomeRow(id: "work0001-aaaa", agentKind: "claude", name: "Sidebar token 统计", preview: "正在跑测试…", state: .working),
                HomeRow(id: "need0001-bbbb", agentKind: "claude", name: "给我绘制一个表格", preview: "想运行一条命令，等你确认", state: .needsYou, needsYou: true),
                HomeRow(id: "stop0001-cccc", agentKind: "claude", name: "Mad ideas", preview: "已停止", state: .stopped, dimmed: true),
                HomeRow(id: "idle0001-dddd", agentKind: "claude", name: "notes"),
                HomeRow(id: "unti0001-eeee", agentKind: "codex", name: "", preview: "ok"),
                HomeRow(id: "cjk00001-ffff", agentKind: "claude", name: "长文本测试",
                        preview: "这是一段很长的中文回复，用来检查列表里的一行预览会在行尾被省略而不是把整行撑宽，也不会把名字挤出屏幕，另外还有一些标点，比如逗号、句号。"),
                HomeRow(id: "emoji001-gggg", agentKind: "claude", name: "🚀 发布助手", preview: "release v1.2 已发布 🎉"),
                HomeRow(id: "long0001-hhhh", agentKind: "claude", name: String(repeating: "很长的名字", count: 12), preview: "name of 60 characters"),
            ]),
            HomeSection(id: "ime", title: "macos-smart-ime", kind: .project(name: "macos-smart-ime"), rows: [
                HomeRow(id: "ime00001-iiii", agentKind: "claude", name: "输入法切换", preview: "已经能在 Ghostty 里自动切到英文了"),
            ]),
        ],
        usage: "Claude 5 小时 42% · 本周 71%")
}

/// The characters, the phone's list and the Mac's sidebar, one at a time.
public struct MadUIGallery: View {
    public enum Page: String, CaseIterable { case characters, home, sidebar }

    @State private var page: Page
    @State private var selection: HomeRow.ID? = "ios"

    public init(page: Page = .characters) { _page = State(initialValue: page) }

    public var body: some View {
        VStack(spacing: 0) {
            Picker("", selection: $page) { ForEach(Page.allCases, id: \.self) { Text($0.rawValue).tag($0) } }
                .pickerStyle(.segmented)
                .padding(8)
            switch page {
            case .characters: CharacterGallery()
            case .home:
                #if os(iOS)
                HomeView(model: SampleHome.model)
                #else
                Text("the phone list is iOS only")
                #endif
            case .sidebar: HomeSidebar(model: SampleHome.model, selection: $selection)
            }
        }
    }
}

#Preview("Gallery") { MadUIGallery(page: .home) }
#endif
