import SwiftUI
import AppKit

/// 设置窗口：配置管理 + 目标服务 + 通用。
struct SettingsView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        @Bindable var model = model

        TabView {
            profilesTab
                .tabItem { Label("代理配置", systemImage: "list.bullet") }
            generalTab
                .tabItem { Label("通用", systemImage: "gearshape") }
        }
        .frame(width: 560, height: 480)
        .sheet(item: $model.editorSheet) { sheet in
            ProfileEditorView(
                profile: sheet.profile,
                isNew: sheet.isNew,
                onSave: { model.saveEditor($0) },
                onCancel: { model.editorSheet = nil }
            )
        }
        .onAppear {
            // accessory app 的设置窗口需要显式激活才有键盘焦点。
            NSApp.activate(ignoringOtherApps: true)
        }
    }

    // MARK: - 配置页

    private var profilesTab: some View {
        VStack(spacing: 0) {
            List {
                ForEach(model.profiles) { profile in
                    HStack {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(profile.name).fontWeight(.medium)
                            Text(summary(of: profile))
                                .font(.caption)
                                .foregroundStyle(.secondary)
                                .lineLimit(1)
                        }
                        Spacer()
                        if model.selection == .profile(profile.id) {
                            Image(systemName: "checkmark.circle.fill")
                                .foregroundStyle(.green)
                        }
                        Button("编辑") { model.beginEdit(profile) }
                    }
                    .padding(.vertical, 2)
                }
                .onDelete { model.delete(at: $0) }
                .onMove { model.move(from: $0, to: $1) }
            }
            HStack {
                Button {
                    model.beginAdd()
                } label: {
                    Label("添加配置", systemImage: "plus")
                }
                Button {
                    Task { await model.prepareImport() }
                } label: {
                    Label("导入当前系统配置", systemImage: "square.and.arrow.down")
                }
                Spacer()
                Text("拖拽排序 · 左滑删除")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .padding(10)
        }
    }

    private func summary(of profile: ProxyProfile) -> String {
        var parts: [String] = []
        if let e = profile.http { parts.append("HTTP \(e.host):\(e.port)") }
        if let e = profile.https { parts.append("HTTPS \(e.host):\(e.port)") }
        if let e = profile.socks { parts.append("SOCKS \(e.host):\(e.port)") }
        if let url = profile.pacURL { parts.append("PAC \(url)") }
        return parts.isEmpty ? "等价直连" : parts.joined(separator: " · ")
    }

    // MARK: - 通用页

    private var generalTab: some View {
        Form {
            Section("目标网络服务") {
                ForEach(model.allServices, id: \.name) { service in
                    Toggle(isOn: overrideBinding(for: service)) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(service.name)
                            Text("\(service.hardwarePort) · \(service.device)\(service.enabled ? "" : " · 已禁用")")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
                Text("默认规则：启用的物理网络服务（排除 iBridge 与桥接设备）。切换时应用到所有勾选的服务。")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Section("通用") {
                Toggle("登录时启动", isOn: launchBinding)
                if let error = model.launchAtLoginError {
                    Text(error).font(.caption).foregroundStyle(.red)
                }
                Text("提示：请先 make install 到 /Applications 再开启登录启动；重新安装后需重新开关一次。")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Section("关于") {
                Text("ProxyBar 0.1.0 — macOS 系统代理快速切换工具")
                    .font(.caption)
                Text("匹配当前状态时忽略绕过域名列表；不支持认证代理。")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .formStyle(.grouped)
    }

    private func overrideBinding(for service: NetworkService) -> Binding<Bool> {
        Binding(
            get: {
                MainActor.assumeIsolated {
                    model.serviceOverrides[service.name] ?? ServiceEnumerator.isDefaultTarget(service)
                }
            },
            set: { newValue in
                Task { @MainActor in model.setOverride(service.name, newValue) }
            }
        )
    }

    private var launchBinding: Binding<Bool> {
        Binding(
            get: { MainActor.assumeIsolated { model.launchAtLoginEnabled } },
            set: { newValue in
                Task { @MainActor in model.setLaunchAtLogin(newValue) }
            }
        )
    }
}