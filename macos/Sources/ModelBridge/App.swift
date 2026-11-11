import SwiftUI

@main
struct ModelBridgeApp: App {
    var body: some Scene {
        MenuBarExtra("模型接入助手") {
            Button("退出") {
                NSApplication.shared.terminate(nil)
            }
        }
    }
}
