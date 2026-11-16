import SwiftUI
import AppKit

/// 菜单栏下拉内容。.menu 风格下 Toggle 映射为原生 NSMenuItem 勾选位。
struct MenuContent: View {
    @Environment(AppModel.self) private var model
    @Environment(\.openSettings) private var openSettings

    var body: some View {
        Toggle(Strings.direct, isOn: selectionBinding(.direct, profile: nil))
            .disabled(model.isApplying)

        Divider()

        ForEach(model.profiles) { profile in
            Toggle(profile.name, isOn: selectionBinding(.profile(profile.id), profile: profile))
                .disabled(model.isApplying)
        }

        if model.selection == .custom {
            // 勾选去哪了：诚实显示当前是不匹配任何配置的自定义态。
            Toggle(Strings.custom, isOn: .constant(true))
                .disabled(true)
        }

        if model.profiles.isEmpty {
            Text(Strings.emptyHint)
        }

        Divider()

        if model.isApplying {
            Text(Strings.applying)
        }

        Button(Strings.importCurrent) {
            Task { @MainActor in
                await model.prepareImport()
                NSApp.activate(ignoringOtherApps: true)
                openSettings()
            }
        }
        .disabled(model.isApplying)

        Button(Strings.refresh) {
            Task { await model.refresh() }
        }
        .disabled(model.isApplying)

        SettingsLink {
            Text(Strings.settings)
        }

        Divider()

        Button(Strings.quit) {
            NSApp.terminate(nil)
        }
    }

    /// Toggle 的 set 忽略传入值，直接应用该行配置（点已勾选行 = 重新应用，无害）。
    private func selectionBinding(_ target: ActiveSelection, profile: ProxyProfile?) -> Binding<Bool> {
        Binding(
            get: { MainActor.assumeIsolated { model.selection == target } },
            set: { _ in
                Task { @MainActor in await model.apply(profile) }
            }
        )
    }
}