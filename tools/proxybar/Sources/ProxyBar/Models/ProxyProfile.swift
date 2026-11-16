import Foundation

/// 一个代理端点（host:port）。
struct ProxyEndpoint: Codable, Hashable, Sendable {
    var host: String
    var port: Int

    var isValid: Bool {
        !host.trimmed.isEmpty && (1...65535).contains(port)
    }

    /// 解析 "host:port"；非法输入返回 nil。按最后一个冒号分割，容忍空白。
    static func parse(_ text: String) -> ProxyEndpoint? {
        let parts = text.trimmed.split(separator: ":", omittingEmptySubsequences: false)
        guard parts.count >= 2,
              let port = Int(parts.last!.trimmed),
              (1...65535).contains(port)
        else { return nil }
        let host = parts.dropLast().joined(separator: ":").trimmed
        guard !host.isEmpty else { return nil }
        return ProxyEndpoint(host: host, port: port)
    }
}

extension StringProtocol {
    var trimmed: String { trimmingCharacters(in: .whitespacesAndNewlines) }
}

/// 一份代理配置：四种类型各自可选，nil 表示应用时该类型必须关闭。
struct ProxyProfile: Codable, Identifiable, Hashable, Sendable {
    var id: UUID
    var name: String
    var http: ProxyEndpoint?
    var https: ProxyEndpoint?
    var socks: ProxyEndpoint?
    var pacURL: String?
    var bypass: [String]

    init(
        id: UUID = UUID(),
        name: String,
        http: ProxyEndpoint? = nil,
        https: ProxyEndpoint? = nil,
        socks: ProxyEndpoint? = nil,
        pacURL: String? = nil,
        bypass: [String] = []
    ) {
        self.id = id
        self.name = name
        self.http = http
        self.https = https
        self.socks = socks
        self.pacURL = pacURL
        self.bypass = bypass
    }

    var isValid: Bool {
        guard !name.trimmed.isEmpty else { return false }
        for endpoint in [http, https, socks].compactMap({ $0 }) {
            guard endpoint.isValid else { return false }
        }
        if let pacURL {
            guard let url = URL(string: pacURL.trimmed),
                  let scheme = url.scheme?.lowercased(),
                  ["http", "https", "file"].contains(scheme)
            else { return false }
        }
        return true
    }
}

enum ProxyKind: String, CaseIterable, Sendable {
    case http, https, socks, pac
}

extension ProxyProfile {
    /// 从系统真实状态生成配置（导入功能）：只取启用类型，忽略禁用残留值。
    static func from(state: SystemProxyState, name: String) -> ProxyProfile {
        ProxyProfile(
            name: name,
            http: state.http.enabled ? ProxyEndpoint(host: state.http.host, port: state.http.port) : nil,
            https: state.https.enabled ? ProxyEndpoint(host: state.https.host, port: state.https.port) : nil,
            socks: state.socks.enabled ? ProxyEndpoint(host: state.socks.host, port: state.socks.port) : nil,
            pacURL: state.pac.enabled ? state.pac.url : nil,
            bypass: state.bypass
        )
    }
}