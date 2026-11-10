import MadKit
import MadUI
import SwiftUI

/// 新团队, in the window's detail pane on the Mac and as a screen over the home list on the iPhone (the page's dialog,
/// natively): the project and template, each role's agent and effort, a name, the goal, the permission and, if wanted,
/// one worktree for all. Starting it opens its channel; a refusal is shown in the form, and what was typed stays.
struct NewTeamDetail: View {
    let api: NewTeamAPI
    let projects: [Project]
    /// Opens the team's channel (after the deck has it).
    let started: (StartedTeam) async -> Void
    var onBack: (() -> Void)?

    @AppStorage("mad.newchat.project") private var projectPath = ""
    @State private var info: TeamTemplates?
    @State private var loadError: String?
    @State private var templateName = ""
    /// Role → its agent and its effort ("" is the default effort).
    @State private var agents: [String: String] = [:]
    @State private var efforts: [String: String] = [:]
    @State private var name = ""
    @State private var goal = ""
    @State private var permission = ""
    @State private var shared = false
    @State private var branch = ""
    @State private var sending = false
    @State private var error: String?

    private var project: Project? { projects.first { $0.path == projectPath } ?? projects.first }
    private var template: TeamTemplate? { info?.templates.first { $0.name == templateName } }

    var body: some View {
        FormPage(title: L("新团队"), note: L("组长和组员会一起在一个新群里启动，目标交给组长。"), onBack: onBack) {
            if let p = project {
                form(p)
            } else {
                Text(L("先添加一个项目")).font(.system(size: 17, weight: .semibold)).foregroundStyle(Color.madFg)
                Text(L("项目就是 agent 工作所在的目录。")).font(.system(size: 15)).foregroundStyle(Color.madFg3)
            }
        }
        .task(id: project?.path) { await load() }
    }

    @ViewBuilder private func form(_ p: Project) -> some View {
        FormField(label: L("项目")) {
            FormMenu(options: projects.map { (value: $0.path, label: $0.name) },
                     selection: Binding(get: { p.path }, set: { projectPath = $0 }))
        }
        if let info, let t = template {
            FormField(label: L("模板")) {
                FormMenu(options: info.templates.map { (value: $0.name, label: $0.label) },
                         selection: Binding(get: { templateName }, set: { templateName = $0; resetRoles() }))
            }
            VStack(spacing: 8) { ForEach(t.roles) { role($0, info) } }
            FormField(label: L("名字")) {
                TextField(L("字母、数字、短横线或任何语言的文字（留空则取自目标）"), text: $name).formText()
            }
            FormField(label: L("目标")) {
                TextField(L("这个小组要完成什么"), text: $goal, axis: .vertical).lineLimit(3...8).formText()
                    .accessibilityIdentifier("newteam.goal")
            }
            FormField(label: L("权限")) {
                FormMenu(options: info.permissions.map { (value: $0, label: permissionLabel($0)) }, selection: $permission)
            }
            Toggle(isOn: $shared) { Text(L("所有人共用一个 worktree（否则各自在项目目录里工作）")).font(.system(size: 14)) }
                .toggleStyle(.switch)
            if shared { TextField(L("分支，例如 team/api"), text: $branch).formText() }
            Text(Lf("同时最多运行 %@ 个小组。", String(info.maxTeams))).font(.system(size: 13)).foregroundStyle(Color.madFg3)
            if let e = info.error { Text(e).font(.system(size: 13)).foregroundStyle(Color.madDanger) }
            FormError(text: error)
            FormSubmit(title: L("启动"), busy: sending ? L("启动中：agent 一个接一个起来…") : nil) { start(p, t) }
                .accessibilityIdentifier("newteam.start")
        } else if let loadError {
            FormError(text: loadError)
        } else if let info, info.templates.isEmpty {
            FormError(text: info.error ?? L("这个项目没有可用的模板。"))
        } else {
            Text(L("正在读取模板…")).font(.system(size: 14)).foregroundStyle(Color.madFg3)
        }
    }

    private func role(_ r: TeamRole, _ info: TeamTemplates) -> some View {
        let agent = agents[r.name] ?? ""
        let levels = info.efforts(of: agent)
        return VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 8) {
                Text(r.name).font(.system(size: 15, weight: .semibold)).foregroundStyle(Color.madFg)
                if r.lead { pill(L("组长")) }
                if r.readOnly { pill(L("只读")) }
            }
            HStack(spacing: 8) {
                FormMenu(options: info.agents(including: TeamAgent.split(r.agent).agent).map {
                    (value: $0, label: $0 == "codex" ? L("codex（用它自己的模型）") : $0)
                }, selection: Binding(get: { agent }, set: { setAgent(r.name, $0, info) }))
                FormMenu(options: [(value: "", label: L("默认强度"))] + levels.map { (value: $0, label: $0) },
                         selection: Binding(get: { levels.contains(efforts[r.name] ?? "") ? efforts[r.name] ?? "" : "" },
                                            set: { efforts[r.name] = $0 }))
            }
        }
    }

    private func pill(_ text: String) -> some View {
        Text(text).font(.system(size: 11, weight: .semibold)).foregroundStyle(Color.madFg2)
            .padding(.horizontal, 7).padding(.vertical, 2).background(Color.madRaise, in: Capsule())
    }

    private func permissionLabel(_ p: String) -> String {
        switch p {
        case "auto": L("auto：由 claude 判断每个操作（推荐）")
        case "allowlist": L("allowlist：改文件、跑测试、构建、提交")
        case "bypass": L("bypass：什么都不问（有风险）")
        default: p
        }
    }

    // MARK: actions

    /// A role's other agent takes the effort it has too, else the default one.
    private func setAgent(_ role: String, _ agent: String, _ info: TeamTemplates) {
        agents[role] = agent
        if !info.efforts(of: agent).contains(efforts[role] ?? "") { efforts[role] = "" }
    }

    /// Each role of the template with its own agent and effort.
    private func resetRoles() {
        agents = [:]
        efforts = [:]
        guard let info, let t = template else { return }
        for r in t.roles {
            let (agent, effort) = (TeamAgent.split(r.agent).agent, TeamAgent.split(r.agent).effort)
            agents[r.name] = agent
            efforts[r.name] = info.efforts(of: agent).contains(effort) ? effort : ""
        }
    }

    private func load() async {
        guard let p = project else { return }
        do {
            let got = try await api.teamTemplates(project: p.path)
            info = got
            loadError = nil
            if !got.templates.contains(where: { $0.name == templateName }) { templateName = got.templates.first?.name ?? "" }
            if !got.permissions.contains(permission) { permission = got.permissions.first ?? "" }
            resetRoles()
        } catch {
            loadError = (error as? LocalizedError)?.errorDescription ?? error.localizedDescription
        }
    }

    private func start(_ p: Project, _ t: TeamTemplate) {
        let g = goal.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !g.isEmpty else {
            error = L("小组需要一个目标。")
            return
        }
        var chosen: [String: String] = [:]
        for r in t.roles { chosen[r.name] = TeamAgent.join(agent: agents[r.name] ?? "", effort: efforts[r.name] ?? "") }
        let go = TeamStart(template: t.name, project: p.path, name: name.trimmingCharacters(in: .whitespacesAndNewlines), goal: g,
                           permission: permission, worktree: shared ? branch.trimmingCharacters(in: .whitespacesAndNewlines) : "",
                           agents: chosen)
        sending = true
        error = nil
        Task {
            defer { sending = false }
            do {
                let team = try await api.startTeam(go)
                await started(team)
            } catch {
                self.error = (error as? LocalizedError)?.errorDescription ?? error.localizedDescription
            }
        }
    }
}
