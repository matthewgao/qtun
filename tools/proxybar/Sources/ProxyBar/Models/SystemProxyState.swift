import Foundation

/// 单个网络服务上某类手动代理的读取态（禁用时 host/port 残留值原样保留）。
struct ProxyTypeState: Codable, Hashable, Sendable {
    var enabled: Bool
    var host: String
    var port: Int
}

struct PACState: Codable, Hashable, Sendable {
    var enabled: Bool
    var url: String?
}

/// 单个网络服务的完整系统代理读取态。
struct SystemProxyState: Codable, Hashable, Sendable {
    var http: ProxyTypeState
    var https: ProxyTypeState
    var socks: ProxyTypeState
    var pac: PACState
    var bypass: [String]

    var isDirect: Bool {
        !http.enabled && !https.enabled && !socks.enabled && !pac.enabled
    }
}

/// 菜单当前选中态。
enum ActiveSelection: Equatable, Sendable {
    case direct
    case profile(UUID)
    case custom
}

/// 一个系统网络服务。
struct NetworkService: Hashable, Sendable {
    var name: String
    var hardwarePort: String
    var device: String
    var enabled: Bool
}