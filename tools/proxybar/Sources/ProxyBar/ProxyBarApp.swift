import SwiftUI
import AppKit

@main
struct ProxyBarApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @State private var model = AppModel.shared

    var body: some Scene {
        MenuBarExtra {
            MenuContent().environment(model)
        } label: {
            Image(systemName: model.menuIconName)
        }
        .menuBarExtraStyle(.menu)

        Settings {
            SettingsView().environment(model)
        }
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_ notification: Notification) {
        // 双保险：即使裸 swift run（无 Info.plist）也不出现 Dock 图标。
        NSApp.setActivationPolicy(.accessory)

        let args = CommandLine.arguments
        if args.contains("--cli-status") || args.contains("--cli-direct") || args.contains("--cli-apply") {
            Task { @MainActor in await CLI.run(args: args) }
            return
        }
        Task { @MainActor in await AppModel.shared.bootstrap() }
    }
}

/// 隐藏调试 CLI：无 UI 驱动真实应用路径，供 E2E 验证与脚本化使用。
/// 用法：ProxyBar --cli-status | --cli-direct | --cli-apply <配置名>
@MainActor
enum CLI {
    struct Status: Codable {
        var selection: String
        var states: [String: SystemProxyState]
    }

    static func run(args: [String]) async -> Never {
        let model = AppModel.shared
        await model.bootstrap()

        if args.contains("--cli-direct") {
            await model.apply(nil)
        }
        if let index = args.firstIndex(of: "--cli-apply"), index + 1 < args.count {
            let name = args[index + 1]
            guard let profile = model.profiles.first(where: { $0.name == name }) else {
                FileHandle.standardError.write(Data("profile not found: \(name)\n".utf8))
                exit(2)
            }
            await model.apply(profile)
        }

        let selectionName: String
        switch model.selection {
        case .direct:
            selectionName = "direct"
        case .profile(let id):
            selectionName = model.profiles.first { $0.id == id }?.name ?? "unknown"
        case .custom:
            selectionName = "custom"
        }
        do {
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
            let data = try encoder.encode(Status(selection: selectionName, states: model.states))
            print(String(decoding: data, as: UTF8.self))
            exit(0)
        } catch {
            FileHandle.standardError.write(Data("encode failed: \(error)\n".utf8))
            exit(1)
        }
    }
}