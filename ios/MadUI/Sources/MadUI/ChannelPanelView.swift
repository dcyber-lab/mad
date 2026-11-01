import SwiftUI

/// The 群信息 panel: the channel's tile, name and topic; its members with how each stands (the lead
/// tagged); the board, the pictures and 继续 when it is paused.
struct ChannelPanelView: View {
    let panel: ChannelPanel
    let actions: ChannelActions
    let openPictures: () -> Void
    let close: () -> Void
    /// A visible way out, for the phone's overlay (the Mac's inspector has its own toggle).
    var showsClose = false

    var body: some View {
        content.overlay(alignment: .topTrailing) {
            if showsClose {
                Button { close() } label: {
                    Image(systemName: "xmark").font(.system(size: 14, weight: .semibold)).foregroundStyle(.secondary)
                        .frame(width: 34, height: 34).background(Color.chatCard, in: Circle())
                }
                .buttonStyle(.plain).accessibilityLabel("关闭群信息").padding(10)
            }
        }
    }

    private var content: some View {
        ScrollView {
            VStack(spacing: 0) {
                VStack(spacing: 8) {
                    GroupTile(members: panel.members.filter { !$0.dim }.map { ($0.id, $0.kind, $0.state) }, size: 64,
                              dot: (panel.members + panel.helpers).contains { $0.state == .needsYou })
                    Text(panel.name).font(MadType.title)
                    if !panel.topic.isEmpty {
                        Text(panel.topic).font(MadType.rowLine).foregroundStyle(.secondary).multilineTextAlignment(.center)
                    }
                }
                .frame(maxWidth: .infinity)
                .padding(.bottom, 16)

                sectionTitle(panel.sectioned ? "小组 \(panel.members.count)" : "成员 · \(panel.members.count)")
                memberList(panel.members)
                if !panel.helpers.isEmpty {
                    sectionTitle("帮手 \(panel.helpers.count)")
                    memberList(panel.helpers)
                }

                sectionTitle("这个群")
                list {
                    if let board = actions.openBoard {
                        actionRow("任务板", detail: "") { close(); board() }
                        Divider().padding(.leading, 12)
                    }
                    actionRow("图片和文件", detail: "\(panel.pictureCount)") { openPictures() }
                    if panel.paused, let resume = actions.resume {
                        Divider().padding(.leading, 12)
                        actionRow("继续", detail: "已暂停", tint: .orange) { resume() }
                    }
                }
            }
            .padding(.horizontal, 18)
            .padding(.vertical, 20)
        }
        .background(Color.chatPanel)
        .accessibilityLabel("群信息")
    }

    private func sectionTitle(_ s: String) -> some View {
        Text(s).font(MadType.label).foregroundStyle(.secondary)
            .frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, 4).padding(.top, 16).padding(.bottom, 6)
    }

    private func list<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        VStack(spacing: 0, content: content)
            .background(Color.chatCard, in: RoundedRectangle(cornerRadius: 14))
            .clipShape(RoundedRectangle(cornerRadius: 14))
    }

    private func memberList(_ rows: [ChannelMember]) -> some View {
        list {
            ForEach(Array(rows.enumerated()), id: \.element.id) { i, m in
                if i > 0 { Divider().padding(.leading, 60) }
                if m.dim {
                    memberRow(m)
                } else {
                    Button { close(); actions.openAgent(m.id) } label: { memberRow(m) }.buttonStyle(.plain)
                }
            }
        }
    }

    private func memberRow(_ m: ChannelMember) -> some View {
        HStack(spacing: 12) {
            CharacterView(id: m.id, kind: m.kind, state: m.state, size: 36)
            VStack(alignment: .leading, spacing: 1) {
                HStack(spacing: 6) {
                    Text(m.role.isEmpty ? m.name : m.role).font(MadType.rowName).lineLimit(1)
                    if m.isLead {
                        Text("负责人").font(MadType.tag).padding(.horizontal, 7).padding(.vertical, 1)
                            .background(Color.chatLine.opacity(0.7), in: Capsule()).foregroundStyle(.secondary)
                    }
                }
                if !m.status.isEmpty {
                    Text(m.status).font(MadType.small).foregroundStyle(.secondary).lineLimit(1)
                }
            }
            Spacer(minLength: 0)
            // its kind, if there is room: it is the kind that gives way
            if !m.kind.isEmpty { Text(m.kind).font(MadType.label).foregroundStyle(.secondary).lineLimit(1).layoutPriority(-1) }
        }
        .padding(.horizontal, 12).padding(.vertical, 9)
        .opacity(m.dim ? 0.55 : 1)
        .contentShape(Rectangle())
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(m.role.isEmpty ? m.name : "\(m.role)，\(m.name)")，\(m.isLead ? "负责人，" : "")\(m.status)")
        .accessibilityAddTraits(m.dim ? [] : .isButton)
    }

    private func actionRow(_ title: String, detail: String, tint: Color = .primary, _ action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack {
                Text(title).font(MadType.message).foregroundStyle(tint)
                Spacer()
                if !detail.isEmpty { Text(detail).font(MadType.small).foregroundStyle(.secondary) }
                Image(systemName: "chevron.right").font(MadType.small.weight(.semibold)).foregroundStyle(.secondary)
            }
            .padding(.horizontal, 12).padding(.vertical, 11)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}
