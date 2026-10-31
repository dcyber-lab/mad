import MadKit
import SwiftUI

/// The Mac's agents by project, working ones first; a team's members sit
/// together under its name.
struct AgentsView: View {
    @ObservedObject var model: MacModel
    let open: (DeckSnapshot.Agent) -> Void

    var body: some View {
        VStack(spacing: 0) {
            LinkBanner(link: model.link)
            content
        }
    }

    @ViewBuilder private var content: some View {
        if let snap = model.snapshot {
            let projects = snap.projects.filter { !$0.agents.isEmpty }
            if projects.isEmpty {
                ContentUnavailableView("No agents", systemImage: "person.2",
                    description: Text("Start one on the Mac; it shows up here."))
            } else {
                List {
                    ForEach(projects) { p in
                        Section(p.name) {
                            let teams = snap.teams.filter { $0.project == p.name }
                            let inTeam = Set(teams.flatMap(\.members))
                            ForEach(sorted(p.agents.filter { !inTeam.contains($0.id) }, snap)) { row($0, snap) }
                            ForEach(teams) { t in
                                let members = sorted(p.agents.filter { t.members.contains($0.id) }, snap)
                                if !members.isEmpty {
                                    DisclosureGroup {
                                        ForEach(members) { row($0, snap) }
                                    } label: {
                                        HStack {
                                            Label(t.name, systemImage: "person.3").font(.subheadline.weight(.medium))
                                            Spacer()
                                            Text(summary(members, snap)).font(.footnote).foregroundStyle(.secondary)
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
                .refreshable { await model.refresh() }
            }
        } else if case .failed = model.link {
            Spacer()
        } else {
            ProgressView().frame(maxHeight: .infinity)
        }
    }

    private func sorted(_ agents: [DeckSnapshot.Agent], _ snap: DeckSnapshot) -> [DeckSnapshot.Agent] {
        agents.enumerated().sorted {
            let (a, b) = (snap.rank(of: $0.element), snap.rank(of: $1.element))
            return a != b ? a < b : $0.offset < $1.offset // the deck's order within a state
        }.map(\.element)
    }

    /// "3 agents", or "3 agents, 1 working".
    private func summary(_ members: [DeckSnapshot.Agent], _ snap: DeckSnapshot) -> String {
        let working = members.filter { snap.tone(for: $0.state) == "working" }.count
        let n = "\(members.count) agent\(members.count == 1 ? "" : "s")"
        return working > 0 ? "\(n), \(working) working" : n
    }

    private func row(_ a: DeckSnapshot.Agent, _ snap: DeckSnapshot) -> some View {
        Button { open(a) } label: {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(a.label).font(.body).foregroundStyle(.primary)
                    if let k = a.kind, k != a.label { Text(k).font(.caption).foregroundStyle(.secondary) }
                }
                Spacer()
                Text(snap.word(for: a.state)).font(.footnote)
                    .foregroundStyle(Tone.color(snap.tone(for: a.state)))
                Image(systemName: "chevron.right").font(.caption.weight(.semibold)).foregroundStyle(.tertiary)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}
