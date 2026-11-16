import SwiftUI

/// 配置编辑表单（新增/编辑/导入草稿共用）。
struct ProfileEditorView: View {
    let isNew: Bool
    let onSave: (ProxyProfile) -> Void
    let onCancel: () -> Void

    private let profileID: UUID
    @State private var name: String
    @State private var httpOn: Bool
    @State private var httpHost: String
    @State private var httpPort: String
    @State private var httpsOn: Bool
    @State private var httpsHost: String
    @State private var httpsPort: String
    @State private var socksOn: Bool
    @State private var socksHost: String
    @State private var socksPort: String
    @State private var pacOn: Bool
    @State private var pacURL: String
    @State private var bypassText: String
    @State private var mixedPort: String = ""

    init(
        profile: ProxyProfile,
        isNew: Bool,
        onSave: @escaping (ProxyProfile) -> Void,
        onCancel: @escaping () -> Void
    ) {
        self.isNew = isNew
        self.onSave = onSave
        self.onCancel = onCancel
        self.profileID = profile.id
        _name = State(initialValue: profile.name)
        _httpOn = State(initialValue: profile.http != nil)
        _httpHost = State(initialValue: profile.http?.host ?? "127.0.0.1")
        _httpPort = State(initialValue: profile.http.map { String($0.port) } ?? "7897")
        _httpsOn = State(initialValue: profile.https != nil)
        _httpsHost = State(initialValue: profile.https?.host ?? "127.0.0.1")
        _httpsPort = State(initialValue: profile.https.map { String($0.port) } ?? "7897")
        _socksOn = State(initialValue: profile.socks != nil)
        _socksHost = State(initialValue: profile.socks?.host ?? "127.0.0.1")
        _socksPort = State(initialValue: profile.socks.map { String($0.port) } ?? "7897")
        _pacOn = State(initialValue: profile.pacURL != nil)
        _pacURL = State(initialValue: profile.pacURL ?? "")
        _bypassText = State(initialValue: profile.bypass.joined(separator: "\n"))
    }

    /// 组装并校验；任何字段非法返回 nil（保存按钮禁用）。
    private var assembled: ProxyProfile? {
        var profile = ProxyProfile(id: profileID, name: name)
        if httpOn {
            guard let endpoint = ProxyEndpoint.parse("\(httpHost):\(httpPort)") else { return nil }
            profile.http = endpoint
        }
        if httpsOn {
            guard let endpoint = ProxyEndpoint.parse("\(httpsHost):\(httpsPort)") else { return nil }
            profile.https = endpoint
        }
        if socksOn {
            guard let endpoint = ProxyEndpoint.parse("\(socksHost):\(socksPort)") else { return nil }
            profile.socks = endpoint
        }
        if pacOn {
            let url = pacURL.trimmed
            guard !url.isEmpty else { return nil }
            profile.pacURL = url
        }
        profile.bypass = bypassText.split(whereSeparator: \.isNewline)
            .map { $0.trimmed }
            .filter { !$0.isEmpty }
        return profile.isValid ? profile : nil
    }

    var body: some View {
        VStack(spacing: 0) {
            Form {
                Section("名称") {
                    TextField("配置名称", text: $name)
                }
                Section("手动代理") {
                    endpointRow("HTTP", on: $httpOn, host: $httpHost, port: $httpPort)
                    endpointRow("HTTPS", on: $httpsOn, host: $httpsHost, port: $httpsPort)
                    endpointRow("SOCKS", on: $socksOn, host: $socksHost, port: $socksPort)
                    HStack {
                        TextField("混合端口（如 127.0.0.1:7897）", text: $mixedPort)
                        Button("填充三端点") { fillMixedPort() }
                    }
                }
                Section("自动代理 (PAC)") {
                    Toggle("启用 PAC", isOn: $pacOn)
                    TextField("PAC URL", text: $pacURL)
                        .disabled(!pacOn)
                }
                Section("绕过域名（每行一条）") {
                    TextEditor(text: $bypassText)
                        .frame(height: 80)
                        .font(.system(.body, design: .monospaced))
                }
            }
            .formStyle(.grouped)

            HStack {
                Spacer()
                Button("取消", action: onCancel)
                    .keyboardShortcut(.cancelAction)
                Button("保存") {
                    if let profile = assembled { onSave(profile) }
                }
                .disabled(assembled == nil)
                .keyboardShortcut(.defaultAction)
            }
            .padding()
        }
        .frame(width: 520, height: 600)
    }

    @ViewBuilder
    private func endpointRow(
        _ title: String,
        on: Binding<Bool>,
        host: Binding<String>,
        port: Binding<String>
    ) -> some View {
        Toggle(title, isOn: on)
        HStack {
            TextField("主机", text: host)
            TextField("端口", text: port)
                .frame(width: 80)
        }
        .disabled(!on.wrappedValue)
    }

    /// Clash 等混合端口：一个 host:port 填进 HTTP/HTTPS/SOCKS 三端点。
    private func fillMixedPort() {
        guard let endpoint = ProxyEndpoint.parse(mixedPort) else { return }
        httpHost = endpoint.host; httpPort = String(endpoint.port); httpOn = true
        httpsHost = endpoint.host; httpsPort = String(endpoint.port); httpsOn = true
        socksHost = endpoint.host; socksPort = String(endpoint.port); socksOn = true
    }
}