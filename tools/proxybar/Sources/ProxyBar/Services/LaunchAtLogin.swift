import Foundation
import ServiceManagement

/// 登录自启（SMAppService.mainApp，macOS 13+）。
/// 状态以系统为准，不做本地持久化。要求 app 位于稳定路径（/Applications）。
@MainActor
enum LaunchAtLogin {
    static var isEnabled: Bool {
        SMAppService.mainApp.status == .enabled
    }

    static func setEnabled(_ on: Bool) throws {
        if on {
            try SMAppService.mainApp.register()
        } else {
            try SMAppService.mainApp.unregister()
        }
    }
}