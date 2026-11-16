import Foundation

/// 纯函数匹配器：系统真实状态 → 当前选中态。不做任何 I/O。
enum ProfileMatcher {

    /// 单服务匹配：只比较配置声明了的类型；未声明的类型必须关闭。
    /// 禁用类型的残留 host/port 忽略；bypass v1 不参与匹配。
    static func matches(_ profile: ProxyProfile, _ state: SystemProxyState) -> Bool {
        func matchEndpoint(_ endpoint: ProxyEndpoint?, _ typeState: ProxyTypeState) -> Bool {
            if let endpoint {
                return typeState.enabled
                    && typeState.host.trimmed.lowercased() == endpoint.host.trimmed.lowercased()
                    && typeState.port == endpoint.port
            }
            return !typeState.enabled
        }
        guard matchEndpoint(profile.http, state.http),
              matchEndpoint(profile.https, state.https),
              matchEndpoint(profile.socks, state.socks)
        else { return false }
        if let pacURL = profile.pacURL {
            return state.pac.enabled && state.pac.url?.trimmed == pacURL.trimmed
        }
        return !state.pac.enabled
    }

    /// 跨服务选择：全直连 → .direct；全服务匹配同一配置 → .profile；否则 .custom。
    static func select(states: [String: SystemProxyState], profiles: [ProxyProfile]) -> ActiveSelection {
        guard !states.isEmpty else { return .custom }
        if states.values.allSatisfy(\.isDirect) { return .direct }
        if let match = profiles.first(where: { p in states.values.allSatisfy { matches(p, $0) } }) {
            return .profile(match.id)
        }
        return .custom
    }
}