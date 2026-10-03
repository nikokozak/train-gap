import SwiftUI

struct RootView: View {
    var body: some View {
        TabView {
            NavigationStack {
                RecordSetupView()
            }
            .tabItem { Label("Record", systemImage: "dot.radiowaves.left.and.right") }

            NavigationStack {
                SessionsView()
            }
            .tabItem { Label("Sessions", systemImage: "list.bullet.rectangle") }
        }
    }
}

